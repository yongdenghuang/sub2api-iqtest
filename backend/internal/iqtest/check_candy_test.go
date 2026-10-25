package iqtest

import "testing"

func TestCheckCandy(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		mode  CandyMode
		want  bool
	}{
		{"plain", "答案是 21", CandyModeAnywhere, true},
		{"with unit", "最少取出21个糖果才能保证。", CandyModeAnywhere, true},
		{"bold", "最终答案：**21**", CandyModeAnywhere, true},
		{"english sentence end", "The answer is 21.", CandyModeAnywhere, true},
		{"empty", "", CandyModeAnywhere, false},
		{"121", "答案是121", CandyModeAnywhere, false},
		{"2021", "2021年的题", CandyModeAnywhere, false},
		{"decimal after", "约 21.5 个", CandyModeAnywhere, false},
		{"decimal before", "约 1.21 个", CandyModeAnywhere, false},
		{"thousands", "21,000", CandyModeAnywhere, false},
		{"percent", "21%", CandyModeAnywhere, false},
		{"identifier", "变量x21", CandyModeAnywhere, false},
		{"ordinal", "21st", CandyModeAnywhere, false},
		{"only in think", "<think>可能是21</think>答案是 22", CandyModeAnywhere, false},
		{"think then answer", "<think>先算</think>答案是 21", CandyModeAnywhere, true},
		{"unclosed think", "<think>推导中出现21，然后被截断", CandyModeAnywhere, false},
		{"anywhere accepts branch", "若先取A需要21，否则需要22。所以答案是22", CandyModeAnywhere, true},
		{"final rejects branch", "若先取A需要21，否则需要22。\n\n所以答案是22", CandyModeFinal, false},
		{"final accepts last paragraph", "推导里提到22。\n\n所以答案是21", CandyModeFinal, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckCandy(tc.reply, tc.mode).Passed; got != tc.want {
				t.Fatalf("CheckCandy(%q, %s) = %v, want %v", tc.reply, tc.mode, got, tc.want)
			}
		})
	}
}
