package msgtype

import "testing"

func TestFixtureCodes(t *testing.T) {
	t.Parallel()
	if Text != 1 || Image != 3 || Voice != 34 || Video != 43 || System != 10000 {
		t.Fatalf("fixture codes drifted: text=%d image=%d voice=%d video=%d system=%d",
			Text, Image, Voice, Video, System)
	}
}

func TestSupportedCatalog(t *testing.T) {
	t.Parallel()
	want := []Code{
		Text, Image, Video, Voice, File, Link, MiniProgram, Card, Location, GIF, Emoji,
		Quote, Merge, RedPacket, Transfer, System, Channels, Live, GroupChain, GroupNotice, Gift,
	}
	if len(want) != 21 {
		t.Fatalf("supported count %d", len(want))
	}
	seenKind := map[string]Code{}
	seenCode := map[Code]string{}
	var supported int
	for _, info := range Catalog() {
		if other, ok := seenKind[info.Kind]; ok {
			t.Fatalf("duplicate kind %s (%d and %d)", info.Kind, other, info.Code)
		}
		seenKind[info.Kind] = info.Code
		if other, ok := seenCode[info.Code]; ok {
			t.Fatalf("duplicate code %d (%s and %s)", info.Code, other, info.Kind)
		}
		seenCode[info.Code] = info.Kind
		if info.Supported {
			supported++
			if info.Code.Placeholder() {
				t.Fatalf("%s should not be a placeholder", info.Kind)
			}
		}
	}
	if supported != 21 {
		t.Fatalf("supported=%d want 21", supported)
	}
	for _, c := range want {
		if !c.Supported() {
			t.Fatalf("%d %s should be supported", c, c.Kind())
		}
	}
	for _, c := range []Code{OANotify, AVCall, App, Unknown} {
		if c.Supported() || !c.Placeholder() {
			t.Fatalf("%s should be a placeholder", c.Kind())
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		msgType int
		extra   map[string]any
		want    Code
	}{
		{1, nil, Text},
		{3, nil, Image},
		{34, nil, Voice},
		{43, nil, Video},
		{10000, nil, System},
		{42, nil, Card},
		{47, nil, Emoji},
		{48, nil, Location},
		{50, nil, AVCall},
		{49, nil, App},
		{49, map[string]any{"kind": "file"}, File},
		{49, map[string]any{"kind": "redpacket"}, RedPacket},
		{49, map[string]any{"app_type": 1002}, Link},
		{49, map[string]any{"app_type": float64(MiniProgram)}, MiniProgram},
		{49, map[string]any{"app_type": "1013"}, Gift},
		{1014, nil, OANotify},
		{0, nil, Unknown},
		{9999, nil, Unknown},
		{1, map[string]any{"kind": "quote"}, Quote},
	}
	for _, tc := range cases {
		got := Parse(tc.msgType, tc.extra)
		if got != tc.want {
			t.Fatalf("Parse(%d, %v)=%d want %d", tc.msgType, tc.extra, got, tc.want)
		}
	}
}

func TestLabels(t *testing.T) {
	t.Parallel()
	if Image.Label() != "图片" || Image.Preview() != "[图片]" {
		t.Fatalf("image labels: %q %q", Image.Label(), Image.Preview())
	}
	if Unknown.Label() == "" || Code(12345).Kind() != "unknown" {
		t.Fatal("unknown fallback")
	}
}
