package quota

import (
	"testing"
	"time"
)

func TestWindowKeyAlignsToWindow(t *testing.T) {
	// 对齐到整小时，保证 1m / 5m / 1h 窗口都在同一时刻开始
	base := time.Unix(1_700_000_000/3600*3600, 0)

	cases := []struct {
		name   string
		window time.Duration
		a, b   time.Time
		same   bool
	}{
		{"默认1分钟同窗", time.Minute, base, base.Add(59 * time.Second), true},
		{"默认1分钟跨窗", time.Minute, base, base.Add(61 * time.Second), false},
		{"5分钟同窗", 5 * time.Minute, base, base.Add(4 * time.Minute), true},
		{"5分钟跨窗", 5 * time.Minute, base, base.Add(5 * time.Minute), false},
		{"1小时同窗", time.Hour, base, base.Add(59 * time.Minute), true},
		{"1小时跨窗", time.Hour, base, base.Add(time.Hour), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rt := NewRedisTPMWithWindow(nil, c.window)
			ka := rt.windowKey(1, c.a)
			kb := rt.windowKey(1, c.b)
			if (ka == kb) != c.same {
				t.Fatalf("window=%s a=%v b=%v: %q vs %q, same=%v", c.window, c.a, c.b, ka, kb, c.same)
			}
		})
	}
}

func TestNextWindow(t *testing.T) {
	base := time.Unix(1_700_000_000/3600*3600, 0)

	cases := []struct {
		window time.Duration
		now    time.Time
		want   time.Time
	}{
		{time.Minute, base, time.Unix((base.Unix()/60+1)*60, 0)},
		{5 * time.Minute, base, time.Unix((base.Unix()/300+1)*300, 0)},
		{time.Hour, base, time.Unix((base.Unix()/3600+1)*3600, 0)},
	}
	for _, c := range cases {
		rt := NewRedisTPMWithWindow(nil, c.window)
		if got := rt.nextWindow(c.now); !got.Equal(c.want) {
			t.Fatalf("window=%s nextWindow=%v want %v", c.window, got, c.want)
		}
		if d := rt.nextWindow(c.now).Sub(c.now); d <= 0 || d > c.window {
			t.Fatalf("window=%s retryAfter=%v 超出 (0, window]", c.window, d)
		}
	}
}

func TestTPMWindowDefaults(t *testing.T) {
	if got := NewRedisTPMWithWindow(nil, 0).window; got != time.Minute {
		t.Fatalf("window=0 应回落 1m, got %v", got)
	}
	if got := NewRedisTPMWithWindow(nil, time.Millisecond).window; got != time.Second {
		t.Fatalf("sub-second 应抬到 1s, got %v", got)
	}
	if got := NewRedisTPM(nil).window; got != time.Minute {
		t.Fatalf("NewRedisTPM 默认应为 1m, got %v", got)
	}
}

func TestTTLSecondsCoversWindow(t *testing.T) {
	cases := []struct {
		window time.Duration
		min    time.Duration
	}{
		{time.Minute, minKeyTTL},
		{5 * time.Minute, minKeyTTL},
		{time.Hour, time.Hour + time.Minute},
		{24 * time.Hour, 24*time.Hour + time.Minute},
	}
	for _, c := range cases {
		rt := NewRedisTPMWithWindow(nil, c.window)
		got := time.Duration(rt.ttlSeconds()) * time.Second
		if got < c.window || got < c.min {
			t.Fatalf("window=%s ttl=%s 太短", c.window, got)
		}
	}
}
