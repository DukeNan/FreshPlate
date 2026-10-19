// argon2_hash.go 供 web/e2e 种子使用：输出与服务端 logic.Passwords.Hash
// 兼容的 argon2id 编码串（Verify 接受 m∈[8,65536]、t∈[1,3]、p=1、16 字节
// salt、32 字节 key；此处用 m=8192/t=1 换取速度）。只依赖 x/crypto，
// 因而可以 `go run` 在模块外的 e2e 目录中运行（internal 包不可跨目录引用）。
// 由 seed-admin.py 调用，配合 sqlite3 INSERT 写入普通用户，绕开
// /auth/register 的每 IP 每小时 10 次限流（既有用例已用满配额）。
package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"golang.org/x/crypto/argon2"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: argon2_hash PASSWORD")
		os.Exit(1)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		fmt.Fprintln(os.Stderr, "argon2_hash:", err)
		os.Exit(1)
	}
	key := argon2.IDKey([]byte(os.Args[1]), salt, 1, 8192, 1, 32)
	fmt.Printf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s\n",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}
