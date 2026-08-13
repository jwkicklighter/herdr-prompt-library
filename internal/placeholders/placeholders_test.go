package placeholders

import (
	"testing"
	"time"
)

func TestExpandSupportedPlaceholders(t *testing.T) {
	snapshot := time.Date(2026, time.August, 13, 17, 42, 9, 0, time.FixedZone("EDT", -4*60*60))
	clockCalls := 0
	expander := Expander{
		Values: Values{
			HerdrTabID:             "tab-7",
			HerdrPluginContextJSON: `{"focused_pane_id":"pane-3"}`,
			Directory:              "/tmp/project",
		},
		Now: func() time.Time {
			clockCalls++
			return snapshot
		},
	}

	input := "{{herdr_tab_id}}|{{ herdr_plugin_context_json }}|{{\t\n today \r\v\f}}|{{now}}|{{      directory  }}|{{today}}"
	want := "tab-7|{\"focused_pane_id\":\"pane-3\"}|2026-08-13|2026-08-13T17:42:09-04:00|/tmp/project|2026-08-13"
	if got := expander.Expand(input); got != want {
		t.Fatalf("Expand() = %q, want %q", got, want)
	}
	if clockCalls != 1 {
		t.Fatalf("clock calls = %d, want one snapshot", clockCalls)
	}
}

func TestExpandLeavesUnsupportedAndMalformedTokensLiteral(t *testing.T) {
	expander := Expander{Now: func() time.Time { return time.Time{} }}
	input := "{{unknown}} {{Today}} {{ today! }} {{today} {{today }} {{{today}}} {{today}}} {{ today\u00a0}} plain {today}"
	if got := expander.Expand(input); got != input {
		t.Fatalf("Expand() = %q, want exact literal %q", got, input)
	}
}

func TestExpandPreservesTextWithoutTokens(t *testing.T) {
	input := "first line\nsecond line  \n$HOME; $(not-a-command) & 'quoted'\t "
	expander := Expander{Now: func() time.Time { return time.Time{} }}
	if got := expander.Expand(input); got != input {
		t.Fatalf("Expand() = %q, want %q", got, input)
	}
}
