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
	User      store.User `json:"user"`
	CSRFToken string     `json:"csrf_token"`
}
type userPage struct {
	Items []store.User `json:"items"`
	Total int          `json:"total"`
}
type auditPage struct {
	Items []store.Audit `json:"items"`
	Total int           `json:"total"`
}
