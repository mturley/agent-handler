package api

import "testing"

func TestParseResourceInput_Slack(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantType    string
		wantID      string
		wantURL     string
		wantErr     bool
	}{
		{
			name:     "archive url restores dot",
			input:    "https://redhat-internal.slack.com/archives/C069KSM8T9N/p1787257539775119",
			wantType: "slack",
			wantID:   "C069KSM8T9N:1787257539.775119",
			wantURL:  "https://redhat-internal.slack.com/archives/C069KSM8T9N/p1787257539775119",
		},
		{
			name:     "thread_ts query param preferred over path",
			input:    "https://acme.slack.com/archives/C0ABC/p1787257539775119?thread_ts=1787257000.111222&cid=C0ABC",
			wantType: "slack",
			wantID:   "C0ABC:1787257000.111222",
			wantURL:  "https://acme.slack.com/archives/C0ABC/p1787257539775119?thread_ts=1787257000.111222&cid=C0ABC",
		},
		{
			name:     "explicit slack prefix",
			input:    "slack:C069KSM8T9N:1787257539.775119",
			wantType: "slack",
			wantID:   "C069KSM8T9N:1787257539.775119",
			wantURL:  "", // no cfg in test → DefaultResourceURL yields ""
		},
		{
			name:    "not a slack thread url (no p-timestamp)",
			input:   "https://acme.slack.com/archives/C0ABC",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotType, gotID, gotURL, err := parseResourceInput(nil, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got type=%q id=%q", tc.input, gotType, gotID)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotType != tc.wantType || gotID != tc.wantID || gotURL != tc.wantURL {
				t.Errorf("parseResourceInput(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.input, gotType, gotID, gotURL, tc.wantType, tc.wantID, tc.wantURL)
			}
		})
	}
}
