package gateway

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// The registry's protocol buckets are settings' own list: a bucket the
// page offers a checkbox for is one the endpoint serves, and in the
// order the settings canonicalize to (routing groups last). Settings
// cannot import gateway to say that at compile time, so this test holds
// the two tables to each other — either side renaming, reordering or
// growing a bucket goes red here first.
func TestAPIJSONBucketsAreRegistryIDs(t *testing.T) {
	var ids []string
	for _, b := range apiJSONBuckets {
		ids = append(ids, b.id)
	}
	if !slices.Equal(ids, settings.RegistryBucketIDs) {
		t.Errorf("apiJSONBuckets' ids %v, want settings.RegistryBucketIDs %v", ids, settings.RegistryBucketIDs)
	}
}
