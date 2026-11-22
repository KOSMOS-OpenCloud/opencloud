package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	userv1beta1 "github.com/cs3org/go-cs3apis/cs3/identity/user/v1beta1"
	provider "github.com/cs3org/go-cs3apis/cs3/storage/provider/v1beta1"
	"github.com/opencloud-eu/reva/v2/pkg/auth/scope"
	revactx "github.com/opencloud-eu/reva/v2/pkg/ctx"
	"github.com/opencloud-eu/reva/v2/pkg/token/manager/jwt"

	"github.com/flash7777/craft-signal-relay/frame"
	relay "github.com/flash7777/craft-signal-relay/signal"

	"github.com/opencloud-eu/opencloud/pkg/log"
	signalhttp "github.com/opencloud-eu/opencloud/services/signal/pkg/server/http"
	"github.com/opencloud-eu/opencloud/services/signal/pkg/service"
)

const (
	secret   = "test-secret"
	editable = "storage$space!editable"
	readonly = "storage$space!readonly"
	hidden   = "storage$space!nobody"
)

// token is what the proxy puts on a request of a signed-in user.
func token(t *testing.T, id, name string) string {
	t.Helper()
	manager, err := jwt.New(map[string]any{"secret": secret, "expires": int64(60)})
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := scope.AddOwnerScope(nil)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := manager.MintToken(context.Background(), &userv1beta1.User{Id: &userv1beta1.UserId{OpaqueId: id, Idp: "test"}, Username: id, DisplayName: name}, scopes)
	if err != nil {
		t.Fatal(err)
	}
	return minted
}

// server is the service's handler over a relay whose rights come from a made-up storage.
func server(t *testing.T, hiddenUsers []string) *httptest.Server {
	t.Helper()
	hooks := service.Hooks{
		Logger: log.NopLogger(),
		Stat: func(r *stdhttp.Request, id *provider.ResourceId) (*provider.ResourceInfo, error) {
			if r.Header.Get(revactx.TokenHeader) == "" {
				return nil, errors.New("stat without the user's token")
			}
			switch id.GetOpaqueId() {
			case "editable":
				return &provider.ResourceInfo{PermissionSet: &provider.ResourcePermissions{Stat: true, InitiateFileDownload: true, InitiateFileUpload: true}}, nil
			case "readonly":
				return &provider.ResourceInfo{PermissionSet: &provider.ResourcePermissions{Stat: true, InitiateFileDownload: true}}, nil
			}
			return nil, errors.New("not found")
		},
		HiddenUsers: hiddenUsers,
	}
	hub := relay.New(hooks.Options(1 << 20))
	srv := httptest.NewServer(signalhttp.Handler(hub, secret))
	t.Cleanup(srv.Close)
	return srv
}

func ticket(t *testing.T, srv *httptest.Server, accessToken, doc string, hiddenTicket bool) (int, relay.TicketResponse) {
	t.Helper()
	body, _ := json.Marshal(relay.TicketRequest{Doc: doc, App: "wordcraft", AppVersion: "1", Mode: relay.ModeEdit, Name: "claimed name", Hidden: hiddenTicket})
	req, _ := stdhttp.NewRequest("POST", srv.URL+"/signal/ticket", bytes.NewReader(body))
	if accessToken != "" {
		req.Header.Set(revactx.TokenHeader, accessToken)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer relay.TicketResponse
	if resp.StatusCode == 200 {
		if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
			t.Fatal(err)
		}
	}
	return resp.StatusCode, answer
}

func TestTicketsFollowTheSignedInUserAndTheirRightsOnTheDocument(t *testing.T) {
	srv := server(t, nil)
	anna := token(t, "anna-id", "Anna Example")

	if status, _ := ticket(t, srv, "", editable, false); status != stdhttp.StatusUnauthorized {
		t.Fatalf("without a token: %d", status)
	}
	if status, answer := ticket(t, srv, anna, editable, false); status != 200 || answer.MaxMode != relay.ModeEdit || answer.Ticket == "" {
		t.Fatalf("may edit: %d %+v", status, answer)
	}
	if status, answer := ticket(t, srv, anna, readonly, false); status != 200 || answer.MaxMode != relay.ModeReadOnly {
		t.Fatalf("may only read: %d %+v", status, answer)
	}
	if status, _ := ticket(t, srv, anna, hidden, false); status != stdhttp.StatusForbidden {
		t.Fatalf("no access: %d", status)
	}
	if status, _ := ticket(t, srv, anna, "not a resource id", false); status != stdhttp.StatusForbidden {
		t.Fatalf("not a document: %d", status)
	}
	// Hidden tickets are switched off unless the instance names who may have one.
	if status, _ := ticket(t, srv, anna, editable, true); status != stdhttp.StatusForbidden {
		t.Fatalf("hidden, switched off: %d", status)
	}
	withRecorder := server(t, []string{"recorder-id"})
	if status, _ := ticket(t, withRecorder, anna, editable, true); status != stdhttp.StatusForbidden {
		t.Fatalf("hidden, not the recorder: %d", status)
	}
	if status, answer := ticket(t, withRecorder, token(t, "recorder-id", "Recorder"), editable, true); status != 200 || answer.MaxMode != relay.ModeReadOnly {
		t.Fatalf("hidden, the recorder: %d %+v", status, answer)
	}
}

func TestAWebSocketJoinsWithItsTicketAndTheSessionShowsTheNameFromTheSignIn(t *testing.T) {
	srv := server(t, nil)
	anna := token(t, "anna-id", "Anna Example")
	_, answer := ticket(t, srv, anna, editable, false)

	// The socket itself carries no token: a browser can't set one. The ticket in the first frame lets it in.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, strings.Replace(srv.URL, "http://", "ws://", 1)+"/signal", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "bye")
	payload, _ := relay.Encode(relay.HelloPayload{Ticket: answer.Ticket, App: "wordcraft", AppVersion: "1", Mode: relay.ModeEdit})
	hello, _ := (&frame.Frame{Type: frame.Hello, Payload: payload}).Encode()
	if err := ws.Write(ctx, websocket.MessageBinary, hello); err != nil {
		t.Fatal(err)
	}
	_, data, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	welcome, err := frame.Decode(data)
	if err != nil || welcome.Type != frame.Welcome {
		t.Fatalf("welcome: %v %+v", err, welcome)
	}

	req, _ := stdhttp.NewRequest("GET", srv.URL+"/signal/sessions?doc="+strings.ReplaceAll(strings.ReplaceAll(editable, "$", "%24"), "!", "%21"), nil)
	req.Header.Set(revactx.TokenHeader, anna)
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sessions relay.SessionsResponse
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&sessions) != nil || len(sessions.Sessions) != 1 {
		t.Fatalf("sessions: %d %+v", resp.StatusCode, sessions)
	}
	info := sessions.Sessions[0]
	if !info.Detail || len(info.Members) != 1 || info.Members[0].Name != "Anna Example" || info.Master != welcome.Sender {
		t.Fatalf("session: %+v", info)
	}
	// Someone who may read the document but has not joined sees that a session runs, not who is in it.
	req.Header.Set(revactx.TokenHeader, token(t, "ben-id", "Ben"))
	other, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Body.Close()
	var brief relay.SessionsResponse
	if other.StatusCode != 200 || json.NewDecoder(other.Body).Decode(&brief) != nil || brief.Sessions[0].Detail || brief.Sessions[0].Members != nil || brief.Sessions[0].Participants != 1 {
		t.Fatalf("brief: %d %+v", other.StatusCode, brief)
	}
}
