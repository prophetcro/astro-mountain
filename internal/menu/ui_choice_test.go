package menu

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestAskChoice(t *testing.T) {
	opts := []string{"icon_seamless", "gfs_seamless", "best_match"}
	def := "icon_seamless"

	cases := []struct {
		name      string
		in        string
		want      string
		wantBack  bool
	}{
		{"数字选第2项", "2\n", "gfs_seamless", false},
		{"回车沿用默认", "\n", "icon_seamless", false},
		{"自由输入未列出的模型名", "ecmwf_ifs04\n", "ecmwf_ifs04", false},
		{"返回上级", "b\n", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUI(context.Background(), bytes.NewReader([]byte(c.in)), &bytes.Buffer{})
			got, err := u.askChoice("气象模式", def, opts)
			if c.wantBack {
				if !errors.Is(err, errBack) {
					t.Fatalf("want errBack, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != c.want {
				t.Fatalf("want %q, got %q", c.want, got)
			}
		})
	}
}

func TestAskCheckbox(t *testing.T) {
	items := []checkboxItem{
		{Label: "同时用 GFS 交叉验证", Checked: false},
		{Label: "启用缓存", Checked: true},
	}

	cases := []struct {
		name     string
		in       string
		want     map[string]bool
		wantBack bool
	}{
		{"回车沿用默认", "\n", map[string]bool{"同时用 GFS 交叉验证": false, "启用缓存": true}, false},
		{"勾选第一项", "1\n", map[string]bool{"同时用 GFS 交叉验证": true, "启用缓存": true}, false},
		{"切换两项", "1,2\n", map[string]bool{"同时用 GFS 交叉验证": true, "启用缓存": false}, false},
		{"返回上级", "b\n", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := newUI(context.Background(), bytes.NewReader([]byte(c.in)), &bytes.Buffer{})
			got, err := u.askCheckbox("测试", items)
			if c.wantBack {
				if !errors.Is(err, errBack) {
					t.Fatalf("want errBack, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Fatalf("want %s=%v, got %v", k, v, got[k])
				}
			}
		})
	}
}
