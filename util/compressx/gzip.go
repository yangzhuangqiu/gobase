package compressx

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// gzipMagicHeader 是 RFC 1952 gzip 数据的固定两字节 magic header。
var gzipMagicHeader = []byte{0x1f, 0x8b}

// DefaultGunzipMaxOutputBytes 是 Gunzip 系列默认入口允许的最大解压结果（单位：byte）。
// 默认 64 MiB；需要其他业务上限时调用 GunzipLimit 系列显式指定。
const DefaultGunzipMaxOutputBytes int64 = 64 * 1024 * 1024

// Gzip 使用 gzip 压缩字节数组。
func Gzip(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, errorsx.Wrap(err, "compressx.Gzip", KindCompressFailed)
	}
	if err := writer.Close(); err != nil {
		return nil, errorsx.Wrap(err, "compressx.Gzip", KindCompressFailed)
	}
	return buffer.Bytes(), nil
}

// Gunzip 使用 gzip 解压字节数组，并应用模块默认输出上限。
// 输出超过 DefaultGunzipMaxOutputBytes 时返回 KindOutputLimitExceeded。
func Gunzip(data []byte) ([]byte, error) {
	return gunzip(data, DefaultGunzipMaxOutputBytes, "compressx.Gunzip")
}

// GunzipLimit 使用 gzip 解压字节数组，并限制最大输出字节数。
//
// 参数:
// - data: gzip 压缩数据。
// - maxOutputBytes: 允许的最大解压结果，必须大于 0；结果恰好等于上限时允许成功。
//
// 返回:
// - 超过上限返回 KindOutputLimitExceeded；上限无效返回 KindInvalidOutputLimit。
func GunzipLimit(data []byte, maxOutputBytes int64) ([]byte, error) {
	if err := validateOutputLimit(maxOutputBytes, "compressx.GunzipLimit"); err != nil {
		return nil, err
	}
	return gunzip(data, maxOutputBytes, "compressx.GunzipLimit")
}

// gunzip 执行 gzip 解压，并强制应用调用入口确定的正数输出上限。
// 阶段角色：统一 reader 创建、受限读取和错误 Kind 映射。
func gunzip(data []byte, maxOutputBytes int64, op string) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errorsx.Wrap(err, op, KindInvalidData)
	}
	defer reader.Close()

	// 受限阶段：最多读取上限字节，再探测一个字节区分“恰好达到上限”和“已经超限”。
	limited := io.LimitReader(reader, maxOutputBytes)
	unzipped, err := io.ReadAll(limited)
	if err != nil {
		return nil, errorsx.Wrap(err, op, KindInvalidData)
	}
	for {
		var probe [1]byte
		count, probeErr := reader.Read(probe[:])
		if count > 0 {
			return nil, errorsx.Newf(op, KindOutputLimitExceeded, "max_output_bytes=%d", maxOutputBytes)
		}
		if probeErr == io.EOF {
			return unzipped, nil
		}
		if probeErr != nil {
			return nil, errorsx.Wrap(probeErr, op, KindInvalidData)
		}
	}
}

// GzipString 压缩字符串并返回字节数组。
func GzipString(value string) ([]byte, error) {
	return Gzip([]byte(value))
}

// GunzipToString 解压 gzip 字节并返回字符串，应用模块默认输出上限。
func GunzipToString(data []byte) (string, error) {
	raw, err := Gunzip(data)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GunzipToStringLimit 受限解压 gzip 字节并返回字符串，适用于容量受控的文本载荷。
// maxOutputBytes 的边界和错误语义与 GunzipLimit 相同。
func GunzipToStringLimit(data []byte, maxOutputBytes int64) (string, error) {
	raw, err := GunzipLimit(data, maxOutputBytes)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// GzipToBase64 压缩并输出 Base64 字符串。
func GzipToBase64(data []byte) (string, error) {
	zipped, err := Gzip(data)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(zipped), nil
}

// GunzipFromBase64 先 Base64 解码，再按模块默认输出上限执行 gzip 解压。
func GunzipFromBase64(value string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errorsx.Wrapf(err, "compressx.GunzipFromBase64", KindInvalidData, "encoding=base64 value_len=%d", len(value))
	}
	return Gunzip(raw)
}

// GunzipFromBase64Limit 先 Base64 解码，再按最大输出字节数执行受限 gzip 解压。
// 适用于接收外部 Base64 gzip 载荷且必须限制解压内存的场景。
func GunzipFromBase64Limit(value string, maxOutputBytes int64) ([]byte, error) {
	if err := validateOutputLimit(maxOutputBytes, "compressx.GunzipFromBase64Limit"); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errorsx.Wrapf(err, "compressx.GunzipFromBase64Limit", KindInvalidData, "encoding=base64 value_len=%d", len(value))
	}
	return gunzip(raw, maxOutputBytes, "compressx.GunzipFromBase64Limit")
}

// validateOutputLimit 校验受限解压入口的容量契约，并保留公开入口 Op。
func validateOutputLimit(maxOutputBytes int64, op string) error {
	if maxOutputBytes <= 0 {
		return errorsx.Newf(op, KindInvalidOutputLimit, "max_output_bytes=%d", maxOutputBytes)
	}
	return nil
}

// IsGzip 判断字节数组是否为 gzip 头。
func IsGzip(data []byte) bool {
	return len(data) >= len(gzipMagicHeader) &&
		data[0] == gzipMagicHeader[0] &&
		data[1] == gzipMagicHeader[1]
}
