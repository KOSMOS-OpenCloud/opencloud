package content

import (
	"context"
	"testing"
)

func TestFeaturesWithout(t *testing.T) {
	features := "docmeta,meta,entities,summary,embedding,store_detect"
	if got := featuresWithout(features, []string{"docmeta"}); got != "meta,entities,summary,embedding,store_detect" {
		t.Errorf("without docmeta = %q", got)
	}
	if got := featuresWithout(features, nil); got != features {
		t.Errorf("nil skip changed features: %q", got)
	}
	if got := featuresWithout("docmeta", []string{"DocMeta"}); got != "" {
		t.Errorf("case-insensitive skip = %q", got)
	}
	ctx := ContextWithoutFeatures(context.Background(), "docmeta")
	if skip, ok := ctx.Value(skipFeaturesKey{}).([]string); !ok || len(skip) != 1 || skip[0] != "docmeta" {
		t.Errorf("context skip features = %v", skip)
	}
}
