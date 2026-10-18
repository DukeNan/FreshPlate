package logic

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestREG001NormalizeUsername(t *testing.T) {
	for _, v := range []struct{ in, want string }{{" Alice_01 ", "alice_01"}, {"KELVIN", "kelvin"}, {"abc", "abc"}, {strings.Repeat("a", 32), strings.Repeat("a", 32)}} {
		got, err := NormalizeUsername(v.in)
		if err != nil || got != v.want {
			t.Fatalf("%q => %q %v", v.in, got, err)
		}
	}
	for _, v := range []string{"ab", strings.Repeat("a", 33), "中abc", "KELVIN", "a bc", "ab-"} {
		if _, err := NormalizeUsername(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
func TestPWD001PasswordBoundaries(t *testing.T) {
	for _, v := range []string{strings.Repeat("a", 12), strings.Repeat("🙂", 128), "  password  "} {
		if err := ValidatePassword(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []string{strings.Repeat("a", 11), strings.Repeat("a", 129), "abcdefghijk\xff"} {
		if ValidatePassword(v) == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	p := newPasswords(8, 1)
	h, err := p.Hash("  password  ")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"password    ", "  PASSWORD  "} {
		ok, err := p.Verify(h, v)
		if err != nil || ok {
			t.Fatalf("changed bytes accepted %v %v", ok, err)
		}
	}
	h, err = p.Hash("éééééééééééé")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := p.Verify(h, strings.Repeat("e\u0301", 12)); ok {
		t.Fatal("normalized unicode")
	}
}
func TestPWD002EncodedHashes(t *testing.T) {
	p := newPasswords(8, 1)
	a, err := p.Hash("long password!")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Hash("long password!")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("salt reused")
	}
	if ok, err := p.Verify(a, "long password!"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for _, h := range []string{"", strings.Repeat("x", 10000), strings.Replace(a, "argon2id", "argon2i", 1), strings.Replace(a, "m=8", "m=4294967295", 1), strings.Replace(a, "t=1", "t=999999", 1), strings.Replace(a, "p=1", "p=255", 1), a + "$extra", a[:len(a)-1]} {
		if ok, err := p.Verify(h, "long password!"); err != nil || ok {
			t.Fatalf("malformed accepted %v %v", ok, err)
		}
	}
	if p.Metrics().InvalidHashes != 8 {
		t.Fatal(p.Metrics())
	}
}
func TestPWD003ProductionParameters(t *testing.T) {
	p := NewPasswords()
	start := time.Now()
	h, err := p.Hash("production password sample")
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if !strings.Contains(h, "$m=65536,t=3,p=1$") {
		t.Fatal("wrong production cost")
	}
	if ok, err := p.Verify(h, "production password sample"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	t.Logf("production hash: %s, Argon2 memory: 64 MiB per slot, maximum concurrent memory: 128 MiB plus runtime", elapsed)
}

func BenchmarkPWD003ProductionHash(b *testing.B) {
	p := NewPasswords()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := p.Hash("production password sample"); err != nil {
			b.Fatal(err)
		}
	}
}
func TestLIMIT003HashSlots(t *testing.T) {
	p := newPasswords(8, 1)
	p.slots <- struct{}{}
	p.slots <- struct{}{}
	if _, err := p.Hash("long password!"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	<-p.slots
	h, err := p.Hash("long password!")
	if err != nil {
		t.Fatal(err)
	}
	p.slots <- struct{}{}
	if _, err := p.Verify(h, "long password!"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	<-p.slots
	<-p.slots
	if ok, err := p.Verify(h, "long password!"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if p.Metrics().CapacityRejected != 2 {
		t.Fatal(p.Metrics())
	}
}
func TestLIMIT001And003Limiter(t *testing.T) {
	l := NewLimiter(2)
	now := time.Unix(100, 0)
	for i := 0; i < 10; i++ {
		if ok, _ := l.Allow("login:name", 10, time.Minute, now); !ok {
			t.Fatal(i)
		}
	}
	if ok, retry := l.Allow("login:name", 10, time.Minute, now.Add(time.Second)); ok || retry != 59 {
		t.Fatal(ok, retry)
	}
	if ok, _ := l.Allow("second", 1, time.Minute, now); !ok {
		t.Fatal("second bucket")
	}
	if ok, _ := l.Allow("third", 1, time.Minute, now); ok {
		t.Fatal("unbounded keys")
	}
	if ok, _ := l.Allow("third", 1, time.Minute, now.Add(time.Minute)); !ok {
		t.Fatal("expired keys retained")
	}
	if m := l.Metrics(); m.RateRejected != 1 || m.CapacityRejected != 1 || m.Keys != 1 {
		t.Fatal(m)
	}
}
func TestLIMIT003ConcurrentLimiter(t *testing.T) {
	l := NewLimiter(10)
	now := time.Unix(1, 0)
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Allow("same", 10, time.Minute, now); ok {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 10 {
		t.Fatal(accepted)
	}
}
