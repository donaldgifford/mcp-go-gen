package cli

import (
	"strings"
	"testing"
)

func TestGenerateOptionsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      generateOptions
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid_new_mode",
			opts: generateOptions{mode: ModeNew, out: "/tmp/x"},
		},
		{
			name: "valid_embed_mode",
			opts: generateOptions{mode: ModeEmbed, out: "./internal/mcp"},
		},
		{
			name: "valid_with_all_bool_flags_set",
			opts: generateOptions{mode: ModeNew, out: "/tmp/x", force: true, dryRun: true},
		},
		{
			name:      "unknown_mode_rejected",
			opts:      generateOptions{mode: "bogus", out: "/tmp/x"},
			wantErr:   true,
			errSubstr: `--mode must be one of`,
		},
		{
			name:      "empty_mode_rejected",
			opts:      generateOptions{mode: "", out: "/tmp/x"},
			wantErr:   true,
			errSubstr: `--mode must be one of`,
		},
		{
			name:      "missing_out_rejected",
			opts:      generateOptions{mode: ModeNew},
			wantErr:   true,
			errSubstr: `--out is required`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.opts.validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validate() = nil, want error containing %q", tt.errSubstr)
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("validate() error = %q, want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() = %v, want nil", err)
			}
		})
	}
}
