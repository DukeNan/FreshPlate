package logic

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestParseQuantityMilli 覆盖数量契约全部合法/非法边界
// （INV-LOGIC-001～016），不经过 float64。
func TestParseQuantityMilli(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
		ok   bool
	}{
		// 合法：整数与小数（INV-LOGIC-001、002）
		{name: "合法整数250", in: "250", want: 250000, ok: true},
		{name: "合法三位小数", in: "250.125", want: 250125, ok: true},
		// 合法：零值与零开头小数（INV-LOGIC-003、004）
		{name: "零值本身", in: "0", want: 0, ok: true},
		{name: "零开头一位小数", in: "0.5", want: 500, ok: true},
		{name: "零开头三位小数", in: "0.125", want: 125, ok: true},
		// 合法：精度与范围边界（INV-LOGIC-005、006、009）
		{name: "最小精度0.001", in: "0.001", want: 1, ok: true},
		{name: "最大数量1000000", in: "1000000", want: 1000000000, ok: true},
		{name: "三位小数0.999", in: "0.999", want: 999, ok: true},
		{name: "三位小数999.999", in: "999.999", want: 999999, ok: true},
		{name: "常用小数1.25", in: "1.25", want: 1250, ok: true},
		{name: "常用小数2.5", in: "2.5", want: 2500, ok: true},

		// 非法：超范围（INV-LOGIC-007）
		{name: "最大值加最小精度", in: "1000000.001", want: 0, ok: false},
		{name: "整数超上限", in: "1000001", want: 0, ok: false},
		// 非法：四位及以上小数，不四舍五入不截断（INV-LOGIC-008）
		{name: "零点四位小数", in: "0.0001", want: 0, ok: false},
		{name: "四位全零小数同样拒绝", in: "0.0000", want: 0, ok: false},
		{name: "整数后四位小数", in: "1.0001", want: 0, ok: false},
		{name: "末位9不进位", in: "250.1239", want: 0, ok: false},
		// 非法：正负号（INV-LOGIC-010）
		{name: "前导加号", in: "+5", want: 0, ok: false},
		{name: "负整数", in: "-1", want: 0, ok: false},
		{name: "负小数", in: "-0.5", want: 0, ok: false},
		{name: "加号零", in: "+0", want: 0, ok: false},
		// 非法：指数形式（INV-LOGIC-011）
		{name: "小写e", in: "1e3", want: 0, ok: false},
		{name: "大写E", in: "1E3", want: 0, ok: false},
		{name: "负指数", in: "1e-3", want: 0, ok: false},
		{name: "零的零次幂形式", in: "0e0", want: 0, ok: false},
		// 非法：空白（INV-LOGIC-012）
		{name: "前导半角空格", in: " 5", want: 0, ok: false},
		{name: "尾随半角空格", in: "5 ", want: 0, ok: false},
		{name: "中间空格", in: "5 .5", want: 0, ok: false},
		{name: "前导制表符", in: "\t250", want: 0, ok: false},
		{name: "前导全角空格", in: "　5", want: 0, ok: false},
		// 非法：前导零（INV-LOGIC-013，"0"/"0.5" 例外已在上半区）
		{name: "三位前导零", in: "007", want: 0, ok: false},
		{name: "一位前导零", in: "01", want: 0, ok: false},
		{name: "双零点小数", in: "00.5", want: 0, ok: false},
		{name: "前导零点小数", in: "01.5", want: 0, ok: false},
		// 非法：残缺形式（INV-LOGIC-014）
		{name: "空串", in: "", want: 0, ok: false},
		{name: "单独小数点", in: ".", want: 0, ok: false},
		{name: "小数点结尾", in: "5.", want: 0, ok: false},
		{name: "小数点开头", in: ".5", want: 0, ok: false},
		{name: "两个小数点", in: "5.1.2", want: 0, ok: false},
		// 非法：非 ASCII 数字（INV-LOGIC-015）
		{name: "全角数字", in: "２５０", want: 0, ok: false},
		{name: "中文数字", in: "五", want: 0, ok: false},
		// 非法：超长整数位不溢出不回绕（INV-LOGIC-016）
		{name: "21个9超int64", in: strings.Repeat("9", 21), want: 0, ok: false},
		{name: "20个9同样拒绝", in: strings.Repeat("9", 20), want: 0, ok: false},
		{name: "非法UTF-8字节", in: "\xff\xfe", want: 0, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseQuantityMilli(tt.in)
			if !tt.ok {
				if err == nil {
					t.Fatalf("ParseQuantityMilli(%q) 应拒绝，却返回 %d", tt.in, got)
				}
				if !errors.Is(err, ErrInvalidArgument) {
					t.Fatalf("ParseQuantityMilli(%q) 错误应为 ErrInvalidArgument，得到 %v", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseQuantityMilli(%q) 不应报错，得到 %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("ParseQuantityMilli(%q) = %d，期望 %d", tt.in, got, tt.want)
			}
		})
	}
}

// TestFormatQuantity 覆盖规范输出（INV-LOGIC-017）与负数入参行为。
// 负数入参选择返回空串：展示路径防御性处理，行为由本测试固化。
func TestFormatQuantity(t *testing.T) {
	tests := []struct {
		name string
		in   int64
		want string
	}{
		{name: "零", in: 0, want: "0"},
		{name: "最小精度1毫", in: 1, want: "0.001"},
		{name: "999毫不补尾零", in: 999, want: "0.999"},
		{name: "整数千毫", in: 1000, want: "1"},
		{name: "1001毫保留末位", in: 1001, want: "1.001"},
		{name: "1250毫去尾零", in: 1250, want: "1.25"},
		{name: "1500毫一位小数", in: 1500, want: "1.5"},
		{name: "999999毫", in: 999999, want: "999.999"},
		{name: "1000000毫整千", in: 1000000, want: "1000"},
		{name: "1005000毫整千", in: 1005000, want: "1005"},
		{name: "上限值", in: 1000000000, want: "1000000"},
		{name: "负数返回空串", in: -1, want: ""},
		{name: "负零值返回空串", in: -1250, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatQuantity(tt.in); got != tt.want {
				t.Fatalf("FormatQuantity(%d) = %q，期望 %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestQuantityRoundTrip 覆盖解析/格式化往返不丢精度（INV-LOGIC-018）：
// 字符串→毫值→字符串，以及毫值→字符串→毫值 双向枚举。
func TestQuantityRoundTrip(t *testing.T) {
	// 规范字符串往返：边界值 0.001 与 1000000 必须包含。
	canonical := []string{
		"0", "0.001", "0.01", "0.125", "0.5", "1", "1.25",
		"2.5", "6", "250", "250.125", "999.999", "1000000",
	}
	for _, s := range canonical {
		m, err := ParseQuantityMilli(s)
		if err != nil {
			t.Fatalf("ParseQuantityMilli(%q) 不应报错，得到 %v", s, err)
		}
		if got := FormatQuantity(m); got != s {
			t.Fatalf("往返丢失精度：ParseQuantityMilli(%q)=%d 再 FormatQuantity 得 %q", s, m, got)
		}
	}

	// 毫值往返：任意合法毫值格式化后必须解析回原值。
	millis := []int64{
		0, 1, 10, 100, 999, 1000, 1001, 1250, 1500,
		999999, 1000000, 1000000000,
	}
	for _, m := range millis {
		s := FormatQuantity(m)
		back, err := ParseQuantityMilli(s)
		if err != nil {
			t.Fatalf("ParseQuantityMilli(FormatQuantity(%d)=%q) 不应报错，得到 %v", m, s, err)
		}
		if back != m {
			t.Fatalf("往返丢失精度：FormatQuantity(%d)=%q 再解析得 %d", m, s, back)
		}
	}
}

// TestParseExpiryDate 覆盖到期日严格解析（INV-LOGIC-019～024 及范围边界）。
func TestParseExpiryDate(t *testing.T) {
	utcDate := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	tests := []struct {
		name string
		in   string
		want time.Time
		ok   bool
	}{
		// 合法（INV-LOGIC-019、023、024）
		{name: "常规日期", in: "2026-10-25", want: utcDate(2026, 10, 25), ok: true},
		{name: "闰年闰日", in: "2028-02-29", want: utcDate(2028, 2, 29), ok: true},
		{name: "范围下界", in: "1970-01-01", want: utcDate(1970, 1, 1), ok: true},
		{name: "范围上界", in: "9999-12-31", want: utcDate(9999, 12, 31), ok: true},

		// 非法：非零填充（INV-LOGIC-020）
		{name: "月份一位", in: "2026-2-3", want: time.Time{}, ok: false},
		{name: "日一位", in: "2026-10-5", want: time.Time{}, ok: false},
		// 非法：无分隔符（INV-LOGIC-021）
		{name: "纯数字串", in: "20261301", want: time.Time{}, ok: false},
		// 非法：带时间后缀与空白（INV-LOGIC-022）
		{name: "ISO时间后缀", in: "2026-10-25T00:00:00", want: time.Time{}, ok: false},
		{name: "空格时间后缀", in: "2026-10-25 10:00", want: time.Time{}, ok: false},
		{name: "前导空白", in: " 2026-10-25", want: time.Time{}, ok: false},
		{name: "尾随空白", in: "2026-10-25 ", want: time.Time{}, ok: false},
		// 非法：非闰年闰日（INV-LOGIC-023）
		{name: "非闰年2月29", in: "2026-02-29", want: time.Time{}, ok: false},
		// 非法：越界与不存在日期（INV-LOGIC-024）
		{name: "早于1970", in: "1969-12-31", want: time.Time{}, ok: false},
		{name: "日越界", in: "9999-12-32", want: time.Time{}, ok: false},
		{name: "零年越界", in: "0000-01-01", want: time.Time{}, ok: false},
		{name: "月越界", in: "2026-13-01", want: time.Time{}, ok: false},
		// 非法：残缺形式
		{name: "空串", in: "", want: time.Time{}, ok: false},
		{name: "缺日", in: "2026-10", want: time.Time{}, ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExpiryDate(tt.in)
			if !tt.ok {
				if err == nil {
					t.Fatalf("ParseExpiryDate(%q) 应拒绝，却返回 %v", tt.in, got)
				}
				if !errors.Is(err, ErrInvalidArgument) {
					t.Fatalf("ParseExpiryDate(%q) 错误应为 ErrInvalidArgument，得到 %v", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExpiryDate(%q) 不应报错，得到 %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("ParseExpiryDate(%q) = %v，期望 %v", tt.in, got, tt.want)
			}
			// 合法输出必须能稳定格式化回同一规范串。
			if s := got.Format(expiryDateLayout); s != tt.in {
				t.Fatalf("ParseExpiryDate(%q) 格式化回 %q，期望与输入一致", tt.in, s)
			}
		})
	}
}
