package app

import (
	"reflect"
	"testing"
)

func TestBriefingSlots(t *testing.T) {
	cases := []struct {
		name      string
		schedules []string
		legacy    string
		want      []string
	}{
		{
			name:      "les schedules gagnent sur le legacy",
			schedules: []string{"08:00", "14:00", "20:00"},
			legacy:    "07:00",
			want:      []string{"08:00", "14:00", "20:00"},
		},
		{
			name:      "legacy seul",
			schedules: nil,
			legacy:    "07:00",
			want:      []string{"07:00"},
		},
		{
			name:      "rien de configuré",
			schedules: nil,
			legacy:    "",
			want:      []string{"07:00"},
		},
		{
			name:      "doublons écartés",
			schedules: []string{"08:00", "08:00"},
			legacy:    "",
			want:      []string{"08:00"},
		},
		{
			name:      "legacy vide ignoré quand schedules présentes",
			schedules: []string{"09:00"},
			legacy:    "",
			want:      []string{"09:00"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := briefingSlots(tc.schedules, tc.legacy)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBriefingSlotsDoesNotMutateInput(t *testing.T) {
	// The copy matters: appending straight onto cfg.Briefing.Schedules
	// would write into the config's backing array, contaminating
	// later reads from the same config.
	in := []string{"08:00", "14:00"}
	before := append([]string(nil), in...)
	_ = briefingSlots(in, "07:00")
	if !reflect.DeepEqual(in, before) {
		t.Errorf("input mutated: got %v, want %v", in, before)
	}
}
