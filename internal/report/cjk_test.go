package report

import "testing"

func TestCharWidth(t *testing.T) {
	cases := []struct {
		name string
		r    rune
		want int
	}{
		{"ASCII 字母", 'A', 1},
		{"ASCII 数字", '7', 1},
		{"半角空格", ' ', 1},
		{"CJK 统一汉字", '云', 2},
		{"CJK 统一汉字/点", '点', 2},
		{"全角括号", '（', 2},
		{"中文标点", '，', 2},
		{"全角字母", 'Ａ', 2},
		{"日文假名", 'あ', 2},
		{"韩文音节", '한', 2},
		{"度符号（窄）", '°', 1},
		{"变体选择符 VS16", '\uFE0F', 0},
		{"组合用附加符号", '\u0301', 0},

		{"零宽连接符", '\u200D', 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CharWidth(c.r); got != c.want {
				t.Fatalf("CharWidth(%q) = %d, want %d", c.r, got, c.want)
			}
		})
	}
}

func TestDispWidth(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"牵牛岗", 6},
		{"点位A", 5},
		{"08-13 22:00", 11},
		{"云底AGL", 7},
	}
	for _, c := range cases {
		if got := DispWidth(c.text); got != c.want {
			t.Fatalf("DispWidth(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}

func TestPad(t *testing.T) {
	if got := Pad("牵牛岗", 10, AlignLeft); DispWidth(got) != 10 {
		t.Fatalf("Pad 左对齐后显示宽度 %d，want 10（%q）", DispWidth(got), got)
	}
	if got := Pad("牵牛岗", 10, AlignLeft); got != "牵牛岗    " {
		t.Fatalf("Pad 左对齐 = %q", got)
	}
	if got := Pad("123", 8, AlignRight); got != "     123" {
		t.Fatalf("Pad 右对齐 = %q", got)
	}

	long := "超长点位名称示例"
	if got := Pad(long, 4, AlignLeft); got != long {
		t.Fatalf("超宽文本被截断：%q", got)
	}
}

func TestRepeat(t *testing.T) {
	if got := Repeat("=", 5); got != "=====" {
		t.Fatalf("Repeat(=,5) = %q", got)
	}
	if got := Repeat("=", 0); got != "" {
		t.Fatalf("Repeat(=,0) = %q", got)
	}
	if got := Repeat("=", -3); got != "" {
		t.Fatalf("Repeat(=,-3) = %q，负数应返回空串", got)
	}
}
