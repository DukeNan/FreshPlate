package logic

import (
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

// 库存与食谱共用的数量/日期纯函数 helper（方案 §3）。
// 不做任何 IO、不依赖 store；数量以千分单位（quantity_milli）整数表示，
// 解析与格式化全程整数运算，绝不经过 float64，避免库存反复编辑引入浮点余量。

// maxQuantityMilli 为数量上限 1000000000，即最大 1000000 原单位，
// 与 inventory_items.quantity_milli 的 CHECK 约束一致。
const maxQuantityMilli int64 = 1000000000

// ParseQuantityMilli 把用户输入的数量字符串解析为千分单位整数。
//
// 只接受 ASCII 十进制：形式为 "0"、或以 [1-9] 开头、可带一个 "." 和 1~3 位小数；
// "0" 本身及 "0.xxx" 小数形式合法，其余前导零（"007"、"00.5"、"01.5"）拒绝。
// 一律拒绝正负号、指数、空白、空串、非法 UTF-8、四位及以上小数（不四舍五入、不截断）。
// 累积与相乘前均检查 int64 溢出，最终结果必须满足 0 <= v <= maxQuantityMilli。
// 所有非法输入统一返回 ErrInvalidArgument。
func ParseQuantityMilli(s string) (int64, error) {
	// 空串与非法 UTF-8 直接拒绝；下方逐字节扫描只放行 ASCII 数字，
	// 此处显式检查用于表达契约（全角数字等合法 UTF-8 也由 ASCII 扫描拒绝）。
	if s == "" || !utf8.ValidString(s) {
		return 0, ErrInvalidArgument
	}
	i := 0
	var intPart int64
	switch c := s[0]; {
	case c == '0':
		// "0" 开头：只允许 "0" 本身或 "0.xxx"，紧跟第二个数字即前导零。
		i = 1
		if i < len(s) && isASCIIDigit(s[i]) {
			return 0, ErrInvalidArgument
		}
	case isASCIIDigit(c):
		// 非零整数开头：逐位累积，×10 前检查溢出，超长整数位拒绝而不回绕。
		for i < len(s) && isASCIIDigit(s[i]) {
			d := int64(s[i] - '0')
			if intPart > (math.MaxInt64-d)/10 {
				return 0, ErrInvalidArgument
			}
			intPart = intPart*10 + d
			i++
		}
	default:
		return 0, ErrInvalidArgument
	}
	var frac int64
	if i < len(s) {
		// 整数部分之后若有内容，必须恰好是一个 "." 加 1~3 位小数直到串尾。
		if s[i] != '.' {
			return 0, ErrInvalidArgument
		}
		i++
		digits := 0
		for i < len(s) && isASCIIDigit(s[i]) {
			if digits == 3 {
				// 第四位及以上小数：直接拒绝。
				return 0, ErrInvalidArgument
			}
			frac = frac*10 + int64(s[i]-'0')
			digits++
			i++
		}
		if digits == 0 || i != len(s) {
			// "." 后无数字，或小数后仍有残余字符（"5."、".5"、"5.1.2"、"1e3" 等）。
			return 0, ErrInvalidArgument
		}
		// 小数左对齐补齐到 3 位（"0.5" → 500，"0.125" → 125）。
		switch digits {
		case 1:
			frac *= 100
		case 2:
			frac *= 10
		}
	}
	// 相乘（×1000）前检查 int64 溢出，之后再做业务范围检查。
	if intPart > (math.MaxInt64-frac)/1000 {
		return 0, ErrInvalidArgument
	}
	v := intPart*1000 + frac
	if v > maxQuantityMilli {
		return 0, ErrInvalidArgument
	}
	return v, nil
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// FormatQuantity 把千分单位整数格式化为规范数量字符串：
// 无多余前导零、去小数末尾零，如 0→"0"、1→"0.001"、1250→"1.25"、1000000000→"1000000"。
// 负数入参选择返回空串而非报错：quantity_milli 由解析层与数据库 CHECK 保证非负，
// 本函数处于纯展示路径，对防御性非法入参静默返回空串（有测试固化该行为）。
func FormatQuantity(m int64) string {
	if m < 0 {
		return ""
	}
	if m == 0 {
		return "0"
	}
	whole := m / 1000
	frac := m % 1000
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	// 小数固定三位补零再去尾随零：1 → "001" → "0.001"，250 → "250" → "1.25"。
	var buf [3]byte
	buf[0] = byte('0' + frac/100)
	buf[1] = byte('0' + frac/10%10)
	buf[2] = byte('0' + frac%10)
	n := 3
	for n > 0 && buf[n-1] == '0' {
		n--
	}
	return strconv.FormatInt(whole, 10) + "." + string(buf[:n])
}

// expiryDateLayout 是到期日唯一接受的严格格式。
const expiryDateLayout = "2006-01-02"

// 到期日允许的范围（UTC 日历日），与方案 §3.3 一致。
var (
	minExpiryDate = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	maxExpiryDate = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
)

// ParseExpiryDate 严格解析 YYYY-MM-DD 到期日。
// time.Parse 之后再 Format 回与原串比对，拒绝非零填充（"2026-2-3"）、
// 带时间后缀、空白以及 "2026-02-29"（非闰年）等一切非规范形式；
// 并限制在 1970-01-01 ~ 9999-12-31 范围内。返回值为 UTC 零点日期。
// 所有非法输入统一返回 ErrInvalidArgument。
func ParseExpiryDate(s string) (time.Time, error) {
	t, err := time.Parse(expiryDateLayout, s)
	if err != nil {
		return time.Time{}, ErrInvalidArgument
	}
	// 严格比对：拦截 time.Parse 可能放行的宽松形式，保证输出与输入逐字符一致。
	if t.Format(expiryDateLayout) != s {
		return time.Time{}, ErrInvalidArgument
	}
	if t.Before(minExpiryDate) || t.After(maxExpiryDate) {
		return time.Time{}, ErrInvalidArgument
	}
	return t, nil
}
