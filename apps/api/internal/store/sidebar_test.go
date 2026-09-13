package store

import (
	"strconv"
	"testing"
)

// Characterization tests pinning NormalizeSidebarPreferencesPatch and
// FilterSidebarChannelOrder, the sidebar validators invoked by both store
// backends before a channel order reaches the database.

func TestNormalizeSidebarPreferencesPatch_NilOrderStaysNil(t *testing.T) {
	out, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{})
	if err != nil {
		t.Fatal(err)
	}
	if out.ChannelOrder != nil {
		t.Fatalf("nil channel order became %#v", out.ChannelOrder)
	}
	if !SidebarPreferencesPatchEmpty(out) {
		t.Fatal("nil channel order is not empty")
	}
}

func TestSidebarPreferencesPatchEmpty_EmptyMapIsNotNil(t *testing.T) {
	if SidebarPreferencesPatchEmpty(SidebarPreferencesPatch{ChannelOrder: map[string][]string{}}) {
		t.Fatal("an empty map should not report as an absent patch")
	}
}

func TestNormalizeSidebarPreferencesPatch_CollapsesDuplicatesToFirstPosition(t *testing.T) {
	out, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{
			"wsp_1": {"chn_a", "chn_b", "chn_a", "chn_c", "chn_b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"chn_a", "chn_b", "chn_c"}
	got := out.ChannelOrder["wsp_1"]
	if len(got) != len(want) {
		t.Fatalf("unexpected order: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected order: %#v", got)
		}
	}
}

func TestNormalizeSidebarPreferencesPatch_DropsUnusableIDs(t *testing.T) {
	long := ""
	for len(long) <= maxSidebarChannelIDLength {
		long += "x"
	}
	out, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{"wsp_1": {"", long, "chn_a"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := out.ChannelOrder["wsp_1"]
	if len(got) != 1 || got[0] != "chn_a" {
		t.Fatalf("unusable ids survived: %#v", got)
	}
}

func TestNormalizeSidebarPreferencesPatch_KeepsClearedWorkspaces(t *testing.T) {
	out, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{"wsp_1": {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	order, ok := out.ChannelOrder["wsp_1"]
	if !ok {
		t.Fatal("a cleared workspace must survive normalization so the store can delete its row")
	}
	if len(order) != 0 {
		t.Fatalf("cleared workspace kept ids: %#v", order)
	}
}

func TestNormalizeSidebarPreferencesPatch_RejectsOversizedInput(t *testing.T) {
	overLimit := make([]string, 0, MaxSidebarChannelOrderIDs+1)
	for i := 0; i <= MaxSidebarChannelOrderIDs; i++ {
		overLimit = append(overLimit, "chn_"+strconv.Itoa(i))
	}
	if _, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{"wsp_1": overLimit},
	}); err == nil {
		t.Fatal("expected the per-workspace cap to reject the patch")
	}

	workspaces := make(map[string][]string, MaxSidebarChannelOrderWorkspaces+1)
	for i := 0; i <= MaxSidebarChannelOrderWorkspaces; i++ {
		workspaces["wsp_"+strconv.Itoa(i)] = []string{"chn_a"}
	}
	if _, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{ChannelOrder: workspaces}); err == nil {
		t.Fatal("expected the workspace-count cap to reject the patch")
	}

	if _, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{"": {"chn_a"}},
	}); err == nil {
		t.Fatal("expected an empty workspace id to be rejected")
	}
}

func TestNormalizeSidebarPreferencesPatch_CountsAfterDeduplication(t *testing.T) {
	atLimit := make([]string, 0, MaxSidebarChannelOrderIDs+1)
	for i := 0; i < MaxSidebarChannelOrderIDs; i++ {
		atLimit = append(atLimit, "chn_"+strconv.Itoa(i))
	}
	atLimit = append(atLimit, "chn_0")
	out, err := NormalizeSidebarPreferencesPatch(SidebarPreferencesPatch{
		ChannelOrder: map[string][]string{"wsp_1": atLimit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ChannelOrder["wsp_1"]) != MaxSidebarChannelOrderIDs {
		t.Fatalf("unexpected length: %d", len(out.ChannelOrder["wsp_1"]))
	}
}

func TestFilterSidebarChannelOrder_KeepsCallerOrderAndDropsStrangers(t *testing.T) {
	got := FilterSidebarChannelOrder(
		[]string{"chn_c", "chn_missing", "chn_a"},
		[]string{"chn_a", "chn_b", "chn_c"},
	)
	want := []string{"chn_c", "chn_a"}
	if len(got) != len(want) {
		t.Fatalf("unexpected filtered order: %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected filtered order: %#v", got)
		}
	}
}

func TestFilterSidebarChannelOrder_EmptyWorkspaceDropsEverything(t *testing.T) {
	if got := FilterSidebarChannelOrder([]string{"chn_a"}, nil); len(got) != 0 {
		t.Fatalf("expected an empty result, got %#v", got)
	}
}
