package logic

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"freshplate/server/internal/store"
)

// 库存模块逻辑层测试（INV-LOGIC-019～036）：日期解析、expiry_state 派生、
// 创建数量/备注/查询码点规则与 today 单次取值。数量契约用例在
// quantity_test.go（INV-LOGIC-001～018）。

// invFixture 构造 service 级夹具：固定时钟、已登录普通用户与一个
// active 食材（鸡胸肉），返回 service、ctx、会话与食材 ID。
func invFixture(t *testing.T, now time.Time) (*Service, context.Context, store.Session, int64) {
	t.Helper()
	s, ctx := setupService(t)
	s.Now = func() time.Time { return now }
	u, err := s.Store.CreateUser(ctx, "alice", "hash-original", "user", s.Now())
	if err != nil {
		t.Fatal(err)
	}
	h := bytes.Repeat([]byte{byte(u.ID)}, 32)
	if _, err = s.Store.IssueSession(ctx, u, h, s.Now(), s.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	actor := store.Session{User: u, TokenHash: h, ExpiresAt: s.Now().Add(time.Hour)}
	r, err := s.Store.DB.Exec("INSERT INTO foods(name,status,created_at,updated_at) VALUES('鸡胸肉','active',1,1)")
	if err != nil {
		t.Fatal(err)
	}
	foodID, err := r.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return s, ctx, actor, foodID
}

// invCode 断言错误为指定 AppError 错误码。
func invCode(t *testing.T, err error, want string) {
	t.Helper()
	var e *store.AppError
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

// INV-LOGIC-019～024：到期日严格解析（格式、闰日与范围边界）。
func TestInvLOGIC019_024_ExpiryDateValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *string
		ok   bool
	}{
		{name: "规范日期", in: sptr("2026-10-25"), ok: true},
		{name: "闰年闰日", in: sptr("2028-02-29"), ok: true},
		{name: "下限1970", in: sptr("1970-01-01"), ok: true},
		{name: "上限9999", in: sptr("9999-12-31"), ok: true},
		{name: "非零填充月日", in: sptr("2026-2-3"), ok: false},
		{name: "非零填充日", in: sptr("2026-10-5"), ok: false},
		{name: "无分隔符", in: sptr("20261301"), ok: false},
		{name: "带时间后缀T", in: sptr("2026-10-25T00:00:00"), ok: false},
		{name: "带时间后缀空格", in: sptr("2026-10-25 10:00"), ok: false},
		{name: "非闰年闰日", in: sptr("2026-02-29"), ok: false},
		{name: "越下限", in: sptr("1969-12-31"), ok: false},
		{name: "越上限", in: sptr("9999-12-32"), ok: false},
		{name: "nil直通", in: nil, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := normalizeInventoryExpiresOn(tc.in)
			if tc.name == "nil直通" {
				if err != nil || out != nil {
					t.Fatalf("nil 应直通: %v %v", out, err)
				}
				return
			}
			if !tc.ok {
				if !errors.Is(err, ErrInvalidArgument) {
					t.Fatalf("应拒绝: %v", err)
				}
				return
			}
			if err != nil || out == nil || *out != *tc.in {
				t.Fatalf("应通过且输出规范一致: %v %v", out, err)
			}
		})
	}
}

// INV-LOGIC-025/027/028/029/030/031：expiry_state 派生与数量零优先级。
func TestInvLOGIC025_027_031_ExpiryStateDerivation(t *testing.T) {
	today := UtcToday(time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name         string
		milli        int64
		expiresOn    *string
		want         string
	}{
		{name: "数量零优先depleted", milli: 0, expiresOn: sptr("2026-10-18"), want: "depleted"},
		{name: "数量零且无日期仍depleted", milli: 0, expiresOn: nil, want: "depleted"},
		{name: "已过期", milli: 1000, expiresOn: sptr("2026-10-18"), want: "expired"},
		{name: "今日到期仍expiring", milli: 1000, expiresOn: sptr("2026-10-19"), want: "expiring"},
		{name: "三天边界expiring", milli: 1000, expiresOn: sptr("2026-10-22"), want: "expiring"},
		{name: "四天边界fresh", milli: 1000, expiresOn: sptr("2026-10-23"), want: "fresh"},
		{name: "更远fresh", milli: 1000, expiresOn: sptr("2026-10-25"), want: "fresh"},
		{name: "无日期unknown", milli: 1000, expiresOn: nil, want: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpiryState(tc.milli, tc.expiresOn, today); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

// INV-LOGIC-032：UTC 日历日跨日，不依赖服务器本地时区。
func TestInvLOGIC032_UTCCalendarDayBoundary(t *testing.T) {
	date := sptr("2026-10-19")
	before := UtcToday(time.Date(2026, 10, 19, 23, 59, 0, 0, time.UTC))
	after := UtcToday(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
	if got := ExpiryState(1000, date, before); got != "expiring" {
		t.Fatalf("23:59 应为 expiring: %s", got)
	}
	if got := ExpiryState(1000, date, after); got != "expired" {
		t.Fatalf("次日 00:00 应为 expired: %s", got)
	}
	if before.Format("2006-01-02") != "2026-10-19" || after.Format("2006-01-02") != "2026-10-20" {
		t.Fatal("UtcToday 未按 UTC 截断日历日")
	}
}

// INV-LOGIC-034：创建数量必须 >0（helper 允许 0，创建业务拒绝）；
// 编辑允许 0 表示用完；food_id/unit 入参校验。
func TestInvLOGIC034_CreateQuantityMustBePositive(t *testing.T) {
	s, ctx, actor, foodID := invFixture(t, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	if _, _, err := s.CreateInventory(ctx, actor, foodID, "g", "0", nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("创建数量 0 应拒绝: %v", err)
	}
	if _, _, err := s.CreateInventory(ctx, actor, 0, "g", "1", nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("food_id=0 应拒绝: %v", err)
	}
	if _, _, err := s.CreateInventory(ctx, actor, foodID, "kg", "1", nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法单位应拒绝: %v", err)
	}
	item, _, err := s.CreateInventory(ctx, actor, foodID, "g", "1", sptr("2026-10-25"), "")
	if err != nil || item.QuantityMilli != 1000 {
		t.Fatalf("创建数量 1 应成功: %+v %v", item, err)
	}
	// 编辑允许 0 表示用完。
	edited, _, err := s.UpdateInventory(ctx, actor, item.ID, item.Version, "0", nil, "")
	if err != nil || edited.QuantityMilli != 0 {
		t.Fatalf("编辑数量 0 应成功: %+v %v", edited, err)
	}
}

// INV-LOGIC-035：note 码点上限（按 Unicode 码点而非字节）。
func TestInvLOGIC035_NoteCodepointLimit(t *testing.T) {
	n500 := strings.Repeat("🍖", 500) // 500 个 emoji = 500 码点、2000 字节
	if out, err := validateInventoryNote(n500); err != nil || out != n500 {
		t.Fatalf("500 码点应通过: %v", err)
	}
	n501 := n500 + "好"
	if _, err := validateInventoryNote(n501); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("501 码点应拒绝")
	}
	if out, err := validateInventoryNote("  备注  "); err != nil || out != "备注" {
		t.Fatalf("应 TrimSpace: %q %v", out, err)
	}
	if _, err := validateInventoryNote(strings.Repeat("a", 501)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("501 ASCII 码点应拒绝")
	}
}

// INV-LOGIC-036：q 码点上限与筛选值/分页校验。
func TestInvLOGIC036_QueryValidation(t *testing.T) {
	q100 := strings.Repeat("好", 100)
	if out, err := ValidateInventoryQuery(q100, "", 1, 20); err != nil || out != q100 {
		t.Fatalf("100 码点应通过: %v", err)
	}
	if _, err := ValidateInventoryQuery(strings.Repeat("好", 101), "", 1, 20); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("101 码点应拒绝")
	}
	if _, err := ValidateInventoryQuery(" 鸡胸 ", "", 1, 20); err != nil {
		t.Fatalf("应 TrimSpace: %v", err)
	}
	if _, err := ValidateInventoryQuery("", "stale", 1, 20); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("非法 expiry_state 应拒绝")
	}
	for _, state := range []string{"depleted", "expired", "expiring", "fresh", "unknown"} {
		if _, err := ValidateInventoryQuery("", state, 1, 20); err != nil {
			t.Fatalf("状态 %s 应通过: %v", state, err)
		}
	}
	if _, err := ValidateInventoryQuery("", "", 0, 20); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("page=0 应拒绝")
	}
	if _, err := ValidateInventoryQuery("", "", 1, 101); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("page_size=101 应拒绝")
	}
}

// INV-LOGIC-026：同一次列表请求内 today 只取一次，SQL 筛选与响应
// 派生共用第一次取得的日期。
func TestInvLOGIC026_TodayTakenOncePerRequest(t *testing.T) {
	s, ctx, actor, foodID := invFixture(t, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	// 今日到期批次：today=10-19 属 expiring；若中途改用 10-20 则变 expired。
	if _, _, err := s.CreateInventory(ctx, actor, foodID, "g", "1", sptr("2026-10-19"), ""); err != nil {
		t.Fatal(err)
	}
	// 更远日期批次：任意 today 下都是 fresh，用于确认列表其余部分正常。
	if _, _, err := s.CreateInventory(ctx, actor, foodID, "g", "1", sptr("2026-10-25"), ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.Now = func() time.Time {
		calls++
		if calls == 1 {
			return time.Date(2026, 10, 19, 23, 59, 0, 0, time.UTC)
		}
		return time.Date(2026, 10, 20, 0, 30, 0, 0, time.UTC)
	}
	items, total, today, err := s.InventoryList(ctx, actor, "", 0, "expiring", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("同一次列表请求应只取一次时钟: %d", calls)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("应只命中今日到期批次: total=%d", total)
	}
	if today.Format("2006-01-02") != "2026-10-19" {
		t.Fatalf("today 应为第一次取得的日期: %s", today)
	}
	if got := ExpiryState(items[0].QuantityMilli, items[0].ExpiresOn, today); got != "expiring" {
		t.Fatalf("筛选与派生应共用同一 today: %s", got)
	}
}

// INV-LOGIC-033：停用食材只能减量（业务规则经 service 层验证）。
func TestInvLOGIC033_DisabledFoodOnlyDecrease(t *testing.T) {
	s, ctx, actor, foodID := invFixture(t, time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC))
	item, _, err := s.CreateInventory(ctx, actor, foodID, "g", "500", sptr("2026-10-25"), "第一批")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec("UPDATE foods SET status='disabled' WHERE id=?", foodID); err != nil {
		t.Fatal(err)
	}
	// 增量拒绝。
	_, _, err = s.UpdateInventory(ctx, actor, item.ID, item.Version, "600", sptr("2026-10-25"), "第一批")
	invCode(t, err, "FOOD_DISABLED")
	// 减量、设零、仅改备注、仅改到期日均允许。
	if _, _, err = s.UpdateInventory(ctx, actor, item.ID, item.Version, "400", sptr("2026-10-25"), "第一批"); err != nil {
		t.Fatalf("减量应允许: %v", err)
	}
	if _, _, err = s.UpdateInventory(ctx, actor, item.ID, item.Version+1, "0", sptr("2026-10-25"), "第一批"); err != nil {
		t.Fatalf("设零应允许: %v", err)
	}
	if _, _, err = s.UpdateInventory(ctx, actor, item.ID, item.Version+2, "0", sptr("2026-10-25"), "仅改备注"); err != nil {
		t.Fatalf("改备注应允许: %v", err)
	}
	if _, _, err = s.UpdateInventory(ctx, actor, item.ID, item.Version+3, "0", sptr("2026-11-01"), "仅改备注"); err != nil {
		t.Fatalf("改到期日应允许: %v", err)
	}
	// 删除也允许。
	if err = s.DeleteInventory(ctx, actor, item.ID, item.Version+4); err != nil {
		t.Fatalf("删除应允许: %v", err)
	}
}
