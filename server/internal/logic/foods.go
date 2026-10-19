package logic

import (
	"context"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"freshplate/server/internal/store"
)

// ValidateFoodInput 校验并规范化食材输入（方案 §3/§4）：
//   - 名称 TrimSpace 后 1~100 码点，拒绝控制字符（\t\n\r 亦视为控制拒绝）；
//   - status 仅 active/disabled；
//   - 营养组六字段（四值+两来源）全空表示未知，否则整组必须齐全，来源
//     TrimSpace 后不得为空白（空字符串不等于 NULL）；能量 0~1000、
//     三营养素 0~100，拒绝 NaN/Inf；零与未知语义保留不改写；
//   - 换算系数存在时必须有非空白换算依据；>0 且 density≤100、
//     piece_weight≤1000000，最多六位小数，拒绝 NaN/Inf；
//   - 过敏原仅接受支持编码、去重后至多 10 个；[]+false=未知、[]+true=已核对
//     无、非空+false=部分已知；声称已核对必须有非空白标注依据；
//   - 四个来源字段按码点限长（500/1000/500/500）。
//
// 全部非法输入统一返回 ErrInvalidArgument。
func ValidateFoodInput(in store.FoodWrite) (store.FoodWrite, error) {
	in.Name = strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 100 {
		return in, ErrInvalidArgument
	}
	for _, r := range in.Name {
		if unicode.IsControl(r) {
			return in, ErrInvalidArgument
		}
	}
	if in.Status != "active" && in.Status != "disabled" {
		return in, ErrInvalidArgument
	}
	// 营养完整组：任一字段出现即要求六字段全部有效。
	values := []*float64{in.EnergyKcal100g, in.ProteinG100g, in.FatG100g, in.CarbohydrateG100g}
	sources := [2]*string{in.NutritionSource, in.NutritionReference}
	empty := true
	for _, v := range values {
		if v != nil {
			empty = false
		}
	}
	for _, s := range sources {
		if s != nil {
			empty = false
		}
	}
	if !empty {
		for _, v := range values {
			if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
				return in, ErrInvalidArgument
			}
		}
		for _, s := range sources {
			if s == nil || strings.TrimSpace(*s) == "" {
				return in, ErrInvalidArgument
			}
		}
		if *in.EnergyKcal100g < 0 || *in.EnergyKcal100g > 1000 {
			return in, ErrInvalidArgument
		}
		for _, v := range []*float64{in.ProteinG100g, in.FatG100g, in.CarbohydrateG100g} {
			if *v < 0 || *v > 100 {
				return in, ErrInvalidArgument
			}
		}
	}
	// 换算系数存在时必须有非空白换算依据；三种来源形态（null/空串/纯空白）均拒绝。
	if in.DensityGml != nil || in.PieceWeightG != nil {
		if in.ConversionSource == nil || strings.TrimSpace(*in.ConversionSource) == "" {
			return in, ErrInvalidArgument
		}
	}
	if in.DensityGml != nil {
		v := *in.DensityGml
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 100 || decimalPlaces(v) > 6 {
			return in, ErrInvalidArgument
		}
	}
	if in.PieceWeightG != nil {
		v := *in.PieceWeightG
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > 1000000 || decimalPlaces(v) > 6 {
			return in, ErrInvalidArgument
		}
	}
	// 过敏原：仅接受 10 种支持编码，先逐项校验再去重，去重后至多 10 个。
	seen := make(map[string]bool, len(in.Allergens))
	deduped := make([]string, 0, len(in.Allergens))
	for _, code := range in.Allergens {
		if !allergenCodes[code] {
			return in, ErrInvalidArgument
		}
		if !seen[code] {
			seen[code] = true
			deduped = append(deduped, code)
		}
	}
	if len(deduped) > 10 {
		return in, ErrInvalidArgument
	}
	in.Allergens = deduped
	// 声称已核对必须有非空白标注依据；撤销完整性时允许保留已知编码。
	if in.AllergenComplete && (in.AllergenSource == nil || strings.TrimSpace(*in.AllergenSource) == "") {
		return in, ErrInvalidArgument
	}
	// 来源字段按码点限长，不依赖数据库字节长度判断。
	for _, item := range []struct {
		value *string
		max   int
	}{
		{in.NutritionSource, 500},
		{in.NutritionReference, 1000},
		{in.ConversionSource, 500},
		{in.AllergenSource, 500},
	} {
		if item.value != nil && utf8.RuneCountInString(*item.value) > item.max {
			return in, ErrInvalidArgument
		}
	}
	return in, nil
}

// decimalPlaces 按 strconv.FormatFloat 的最短 'f' 表示统计十进制小数位；
// 'f' 格式不产生指数形式，长度即可靠。
func decimalPlaces(v float64) int {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	return len(s) - i - 1
}

// ValidateFoodQuery 校验食材列表筛选参数：q TrimSpace 后至多 100 码点，
// status 仅接受 active/disabled/空，page≥1、page_size 1~100（与现有分页
// 上限一致）。返回裁剪后的 q。
func ValidateFoodQuery(q, status string, page, size int) (string, error) {
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 100 {
		return "", ErrInvalidArgument
	}
	if status != "" && status != "active" && status != "disabled" {
		return "", ErrInvalidArgument
	}
	if page < 1 || page > 1000000 {
		return "", ErrInvalidArgument
	}
	if size < 1 || size > 100 {
		return "", ErrInvalidArgument
	}
	return q, nil
}

// FoodCatalog 登录用户检索 active 食材摘要分页。
func (s *Service) FoodCatalog(ctx context.Context, q string, page, size int) ([]store.FoodSummary, int, error) {
	q, err := ValidateFoodQuery(q, "", page, size)
	if err != nil {
		return nil, 0, err
	}
	return s.Store.FoodCatalog(ctx, q, page, size)
}

// AdminFoodList 管理员检索全部状态食材摘要分页。
func (s *Service) AdminFoodList(ctx context.Context, q, status string, page, size int) ([]store.FoodSummary, int, error) {
	q, err := ValidateFoodQuery(q, status, page, size)
	if err != nil {
		return nil, 0, err
	}
	return s.Store.FoodAdminList(ctx, q, status, page, size)
}

// Food 读取食材完整详情；停用食材同样可读。ID 必须为正整数。
func (s *Service) Food(ctx context.Context, id int64) (store.FoodDetail, error) {
	if id <= 0 {
		return store.FoodDetail{}, ErrInvalidArgument
	}
	return s.Store.FoodByID(ctx, id)
}

// CreateFood 校验后创建食材（version=1）并写创建审计。
func (s *Service) CreateFood(ctx context.Context, actor store.Session, in store.FoodWrite) (store.FoodDetail, error) {
	in, err := ValidateFoodInput(in)
	if err != nil {
		return store.FoodDetail{}, err
	}
	return s.Store.CreateFood(ctx, actor, in, s.Now())
}

// UpdateFood 校验后全量替换食材可编辑字段；version 为客户端持有的当前
// 乐观并发版本。
func (s *Service) UpdateFood(ctx context.Context, actor store.Session, id, version int64, in store.FoodWrite) (store.FoodDetail, error) {
	if version <= 0 {
		return store.FoodDetail{}, ErrInvalidArgument
	}
	in, err := ValidateFoodInput(in)
	if err != nil {
		return store.FoodDetail{}, err
	}
	return s.Store.UpdateFood(ctx, actor, id, version, in, s.Now())
}
