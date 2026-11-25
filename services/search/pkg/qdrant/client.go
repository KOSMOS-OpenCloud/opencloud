// Package qdrant provides a lightweight client for the Qdrant vector database REST API.
// Used by the search service to store document embeddings from open_taki v2.
package qdrant

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/opencloud-eu/opencloud/pkg/log"
)

// Client is a minimal Qdrant REST API client.
type Client struct {
	url        string
	collection string
	httpClient *http.Client
	logger     log.Logger
}

// Point represents a vector point to upsert into Qdrant.
type Point struct {
	ID      string                 `json:"id"`
	Vector  []float64              `json:"vector"`
	Payload map[string]interface{} `json:"payload"`
}

type upsertRequest struct {
	Points []Point `json:"points"`
}

type collectionConfig struct {
	Vectors struct {
		Size     int    `json:"size"`
		Distance string `json:"distance"`
	} `json:"vectors"`
}

// New creates a new Qdrant client.
func New(url, collection string, logger log.Logger) *Client {
	return &Client{
		url:        url,
		collection: collection,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger,
	}
}

// EnsureCollection creates the collection if it doesn't exist.
func (c *Client) EnsureCollection(vectorSize int) error {
	// Check if collection exists
	resp, err := c.httpClient.Get(fmt.Sprintf("%s/collections/%s", c.url, c.collection))
	if err != nil {
		return fmt.Errorf("qdrant: checking collection: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == 200 {
		return nil // exists
	}

	// Create collection
	cfg := collectionConfig{}
	cfg.Vectors.Size = vectorSize
	cfg.Vectors.Distance = "Cosine"

	body, _ := json.Marshal(cfg)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/collections/%s", c.url, c.collection), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err = c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: creating collection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant: create collection failed (%d): %s", resp.StatusCode, string(respBody))
	}

	c.logger.Info().Str("collection", c.collection).Int("vector_size", vectorSize).Msg("qdrant: collection created")
	return nil
}

// Upsert inserts or updates points in the collection.
func (c *Client) Upsert(points []Point) error {
	if len(points) == 0 {
		return nil
	}

	body, _ := json.Marshal(upsertRequest{Points: points})
	req, _ := http.NewRequest("PUT",
		fmt.Sprintf("%s/collections/%s/points", c.url, c.collection),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant: upsert: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("qdrant: upsert failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Search performs a vector similarity search.
func (c *Client) Search(vector []float64, limit int) ([]SearchResult, error) {
	payload := map[string]interface{}{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
		// resources in the trash bin keep their point, but are no hits
		"filter": map[string]interface{}{
			"must_not": []map[string]interface{}{
				{"key": DeletedPayloadKey, "match": map[string]interface{}{"value": true}},
			},
		},
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/collections/%s/points/search", c.url, c.collection),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: search: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result []SearchResult `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Result, nil
}

// SearchResult represents a single search hit.
type SearchResult struct {
	ID      string                 `json:"id"`
	Score   float64                `json:"score"`
	Payload map[string]interface{} `json:"payload"`
}

// DeletedPayloadKey marks a point whose resource is in the trash bin.
const DeletedPayloadKey = "deleted"

// pointsPerRequest limits how many points or operations one request carries.
const pointsPerRequest = 500

// PayloadUpdate sets payload fields on the given points. Points that do not
// exist are skipped.
type PayloadUpdate struct {
	IDs     []string
	Payload map[string]interface{}
}

// hasIDFilter selects points by id. Unlike a plain id list it does not fail
// for ids without a point (folders and files that were never embedded).
func hasIDFilter(ids []string) map[string]interface{} {
	return map[string]interface{}{
		"must": []map[string]interface{}{{"has_id": ids}},
	}
}

func (c *Client) post(operation, path string, requestBody interface{}) ([]byte, error) {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("qdrant: %s: %w", operation, err)
	}
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/collections/%s/%s", c.url, c.collection, path),
		bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("qdrant: %s: %w", operation, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("qdrant: %s: %w", operation, err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qdrant: %s failed (%d): %s", operation, resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// Delete removes the points with the given ids. Ids without a point are ignored.
func (c *Client) Delete(ids []string) error {
	for start := 0; start < len(ids); start += pointsPerRequest {
		end := min(start+pointsPerRequest, len(ids))
		if _, err := c.post("delete", "points/delete", map[string]interface{}{
			"filter": hasIDFilter(ids[start:end]),
		}); err != nil {
			return err
		}
	}
	return nil
}

// SetPayload merges payload fields into existing points, other fields stay.
func (c *Client) SetPayload(updates []PayloadUpdate) error {
	operations := make([]map[string]interface{}, 0, len(updates))
	for _, update := range updates {
		for start := 0; start < len(update.IDs); start += pointsPerRequest {
			end := min(start+pointsPerRequest, len(update.IDs))
			operations = append(operations, map[string]interface{}{
				"set_payload": map[string]interface{}{
					"payload": update.Payload,
					"filter":  hasIDFilter(update.IDs[start:end]),
				},
			})
		}
	}
	for start := 0; start < len(operations); start += pointsPerRequest {
		end := min(start+pointsPerRequest, len(operations))
		if _, err := c.post("set payload", "points/batch", map[string]interface{}{
			"operations": operations[start:end],
		}); err != nil {
			return err
		}
	}
	return nil
}

// Retrieve returns the point with the given id including vector and payload.
// The second return value is false if there is no such point.
func (c *Client) Retrieve(id string) (Point, bool, error) {
	respBody, err := c.post("retrieve", "points", map[string]interface{}{
		"ids":          []string{id},
		"with_payload": true,
		"with_vector":  true,
	})
	if err != nil {
		return Point{}, false, err
	}
	var result struct {
		Result []Point `json:"result"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return Point{}, false, fmt.Errorf("qdrant: retrieve: %w", err)
	}
	if len(result.Result) == 0 {
		return Point{}, false, nil
	}
	return result.Result[0], true, nil
}
