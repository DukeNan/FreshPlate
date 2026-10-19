package api

import (
	"encoding/json"
	"freshplate/server/internal/store"
)

type credentials struct {
	Username string `json:"username" binding:"required" example:"alice_01"`                                         // 去除首尾空白后为 3～32 位 ASCII 字母、数字或下划线，保存为小写。
	Password string `json:"password" binding:"required" minLength:"12" maxLength:"128" example:"Example-pass-2026"` // UTF-8 不超过 512 字节，不裁剪或规范化。
}

type passwordChange struct {
	Current string `json:"current_password" binding:"required" minLength:"12" maxLength:"128"`
	New     string `json:"new_password" binding:"required" minLength:"12" maxLength:"128"` // UTF-8 不超过 512 字节，不能与当前密码相同。
}

type userUpdate struct {
	Role   *string `json:"role" enums:"user,admin"`
	Status *string `json:"status" enums:"active,disabled"`
}

type passwordReset struct {
	Password string `json:"temporary_password" binding:"required" minLength:"12" maxLength:"128"` // UTF-8 不超过 512 字节，不能与目标当前密码相同。
}

type preferencesInput struct {
	Allergens          *[]string       `json:"allergens" binding:"required" maxItems:"32" enums:"milk,egg,fish,crustacean,mollusc,peanut,soy,wheat,tree_nut,sesame"`
	ExcludedFoodIDs    *[]int64        `json:"excluded_food_ids" binding:"required" maxItems:"500"` // 正整数，服务端去重；不能新增停用食材。
	DietaryPreferences *[]string       `json:"dietary_preferences" binding:"required" maxItems:"32" enums:"vegetarian,vegan,low_salt"`
	Energy             json.RawMessage `json:"daily_energy_kcal" binding:"required" swaggertype:"number" minimum:"0" maximum:"20000" extensions:"x-nullable"`   // null 或有限正数，不含 0。
	Protein            json.RawMessage `json:"daily_protein_g" binding:"required" swaggertype:"number" minimum:"0" maximum:"2000" extensions:"x-nullable"`      // null 或有限正数，不含 0。
	Fat                json.RawMessage `json:"daily_fat_g" binding:"required" swaggertype:"number" minimum:"0" maximum:"2000" extensions:"x-nullable"`          // null 或有限正数，不含 0。
	Carbohydrate       json.RawMessage `json:"daily_carbohydrate_g" binding:"required" swaggertype:"number" minimum:"0" maximum:"2000" extensions:"x-nullable"` // null 或有限正数，不含 0。
}

type response struct {
	Data any `json:"data"`
}
type errorDetail struct {
	Code    string `json:"code" example:"AUTH_REQUIRED"`
	Message string `json:"message" example:"请先登录"`
}
type errorResponse struct {
	Error errorDetail `json:"error"`
}
type csrfResult struct {
	Token string `json:"token"`
}
type loginResult struct {
	AccessToken string     `json:"access_token"`
	TokenType   string     `json:"token_type"`
	ExpiresIn   int        `json:"expires_in"`
	User        store.User `json:"user"`
	CSRFToken   string     `json:"csrf_token"`
}
type userPage struct {
	Items []store.User `json:"items"`
	Total int          `json:"total"`
}
type auditPage struct {
	Items []store.Audit `json:"items"`
	Total int           `json:"total"`
}

// foodInput 是食材创建输入；POST/PUT 全字段必填，未知信息显式传 null 或
// false。id、version、created_at、updated_at 等服务端字段不在 DTO 中，
// 出现即被 decode 拒绝。
//
// 注意：本项目运行时不用 gin 绑定校验，而是由 decodeFoodInput 用
// encoding/json 自行解码（键必填、值可 null）；下列 binding:"required"
// 仅作为 Swagger 元数据（swag 会把它写进 schema 的 required 数组），
// 无任何运行时效果，请勿据其推断校验行为。
type foodInput struct {
	Name               string    `json:"name" binding:"required" example:"燕麦片"` // 去除首尾空白后 1～100 码点，不含控制字符。
	Status             string    `json:"status" binding:"required" enums:"active,disabled"`
	EnergyKcal100g     *float64  `json:"energy_kcal_100g" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"1000"`                               // 键必填；值可为 null 表示未知。营养组六字段必须全空或全有。
	ProteinG100g       *float64  `json:"protein_g_100g" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"100"`                                  // 键必填；值可为 null 表示未知。
	FatG100g           *float64  `json:"fat_g_100g" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"100"`                                      // 键必填；值可为 null 表示未知。
	CarbohydrateG100g  *float64  `json:"carbohydrate_g_100g" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"100"`                             // 键必填；值可为 null 表示未知。
	NutritionSource    *string   `json:"nutrition_source" binding:"required" extensions:"x-nullable" maxLength:"500"`                                          // 键必填；值可为 null 表示未知。营养组存在时必填；仅空白视为缺失。
	NutritionReference *string   `json:"nutrition_reference" binding:"required" extensions:"x-nullable" maxLength:"1000"`                                      // 键必填；值可为 null 表示未知。营养组存在时必填；仅空白视为缺失。
	DensityGml         *float64  `json:"density_g_ml" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"100"`                                    // 键必填；值可为 null 表示未知。>0，最多六位小数；存在时必须有换算依据。
	PieceWeightG       *float64  `json:"piece_weight_g" binding:"required" extensions:"x-nullable" minimum:"0" maximum:"1000000"`                              // 键必填；值可为 null 表示未知。>0，最多六位小数；存在时必须有换算依据。
	ConversionSource   *string   `json:"conversion_source" binding:"required" extensions:"x-nullable" maxLength:"500"`                                         // 键必填；值可为 null 表示未知。任一换算系数存在时必填。
	Allergens          *[]string `json:"allergens" binding:"required" maxItems:"10" enums:"milk,egg,fish,crustacean,mollusc,peanut,soy,wheat,tree_nut,sesame"` // 去重后至多 10 个；[] 表示无已知标注。
	AllergenComplete   *bool     `json:"allergen_complete" binding:"required"`                                                                                 // true=已核对；必须提供过敏原标注依据。
	AllergenSource     *string   `json:"allergen_source" binding:"required" extensions:"x-nullable" maxLength:"500"`                                           // 键必填；值可为 null 表示未知。声称已核对时必填。
}

// foodUpdateInput 是食材更新输入：与创建相同，并要求提交当前乐观并发版本。
type foodUpdateInput struct {
	foodInput
	Version *int64 `json:"version" binding:"required" minimum:"1"` // 服务端按 id+version 条件更新，不一致返回 409。
}

type foodPage struct {
	Items []store.FoodSummary `json:"items"`
	Total int                 `json:"total"`
}
type resourceAuditPage struct {
	Items []store.ResourceAudit `json:"items"`
	Total int                   `json:"total"`
}
