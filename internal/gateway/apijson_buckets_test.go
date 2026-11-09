package gateway

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// The registry's wires are settings' own list: a wire the settings page
// offers a tick for is one the endpoint serves, and in the order the
// settings canonicalize to. The routing groups are not among them — their
// entry's id is settings' own constant, which this holds too. Settings
// cannot import gateway to say that at compile time, so this test holds
// the two tables to each other — either side renaming, reordering or
// growing a wire goes red here first.
func TestAPIJSONWiresAreRegistryWireIDs(t *testing.T) {
	var ids []string
	for _, w := range apiJSONWires {
		ids = append(ids, w.id)
	}
	if !slices.Equal(ids, settings.RegistryWireIDs) {
		t.Errorf("apiJSONWires' ids %v, want settings.RegistryWireIDs %v", ids, settings.RegistryWireIDs)
	}
	if apiJSONGroups.id != settings.RegistryGroupID {
		t.Errorf("the groups entry's id %q, want settings.RegistryGroupID %q", apiJSONGroups.id, settings.RegistryGroupID)
	}
}
