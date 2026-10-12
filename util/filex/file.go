package filex

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// utf8BOM 是 UTF-8 BOM 固定三字节前缀，仅用于显式检测和切片移除。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// lineReadResult 表示一次读行推进结果，OK=false 且 Err=nil 的等价状态由调用方视为 EOF。
type lineReadResult struct {
	// Line 是去掉 LF/CRLF 的当前行文本；OK=false 时为空。
	Line string
	// OK 表示本次读取产生了一行，包括内容为空的有效空行。
	OK bool
	// TooLong 表示当前行内容超过模块固定上限，调用方应返回 KindLineTooLong。
	TooLong bool
}

const (
	// defaultMaxLineBytes 是 ReadLines / ReadLinesBatch 的单行默认上限（单位: byte）。
	defaultMaxLineBytes = 1 * 1024 * 1024
	// defaultReadBufferBytes 是逐行读取时的基础缓冲大小（单位: byte）。
	defaultReadBufferBytes = 64 * 1024
)

// Exists 判断路径是否存在。
func Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, errorsx.Wrapf(err, "filex.Exists", KindIOError, "stage=stat path=%s", path)
}

// EnsureDir 显式创建目录（已存在则直接返回）。
func EnsureDir(path string) error {
	if path == "" {
		return errorsx.Newf("filex.EnsureDir", KindInvalidArgument, "field=path")
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return errorsx.Wrapf(err, "filex.EnsureDir", KindIOError, "stage=mkdir_all path=%s", path)
	}
	return nil
}

// ReadLines 读取文件全部文本行（按行切分，不包含行尾换行符）。
// 语义:
// - 默认单行最大 1MB，超限返回 KindLineTooLong。
func ReadLines(filePath string) ([]string, error) {
	// 资源阶段：打开后同步读取，函数返回时不保留文件句柄或后台任务。
	file, err := os.Open(filePath)
	if err != nil {
		return nil, errorsx.Wrapf(err, "filex.ReadLines", KindIOError, "stage=open path=%s", filePath)
	}
	defer file.Close()

	lines := make([]string, 0)
	reader := bufio.NewReaderSize(file, defaultReadBufferBytes)
	// 读取阶段：EOF 由 OK=false 表达，超长行与底层 I/O 使用不同 Kind。
	for {
		result, readErr := readLineWithLimit(reader, defaultMaxLineBytes)
		if readErr != nil {
			return nil, errorsx.Wrapf(readErr, "filex.ReadLines", KindIOError, "stage=read_line path=%s", filePath)
		}
		if result.TooLong {
			return nil, errorsx.Newf(
				"filex.ReadLines",
				KindLineTooLong,
				"path=%s max=%d",
				filePath,
				defaultMaxLineBytes,
			)
		}
		if !result.OK {
			break
		}
		lines = append(lines, result.Line)
	}
	return lines, nil
}

// ReadString 读取文件全部内容并自动去除 UTF-8 BOM。
func ReadString(filePath string) (string, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", errorsx.Wrapf(err, "filex.ReadString", KindIOError, "stage=read_file path=%s", filePath)
	}
	cleaned, _ := RemoveUTF8BOM(raw)
	return string(cleaned), nil
}

// ReadBytes 读取文件全部字节内容。
func ReadBytes(filePath string) ([]byte, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errorsx.Wrapf(err, "filex.ReadBytes", KindIOError, "stage=read_file path=%s", filePath)
	}
	return raw, nil
}

// ReadLinesBatch 按批次读取文件行并回调处理。
// 语义:
// - batchSize 表示每批处理的行数。
// - 默认单行最大 1MB，超限返回 KindLineTooLong。
func ReadLinesBatch(filePath string, batchSize int, handle func(lines []string) error) error {
	// 参数阶段：在打开文件前 fail-fast，避免无效调用产生文件系统副作用。
	if batchSize <= 0 {
		return errorsx.Newf("filex.ReadLinesBatch", KindInvalidArgument, "field=batch_size value=%d", batchSize)
	}
	if handle == nil {
		return errorsx.Newf("filex.ReadLinesBatch", KindInvalidArgument, "field=handle")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return errorsx.Wrapf(err, "filex.ReadLinesBatch", KindIOError, "stage=open path=%s", filePath)
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, defaultReadBufferBytes)
	batch := make([]string, 0, batchSize)
	// 批处理阶段：完整批次同步交给回调；回调成功后创建新切片，允许调用方保留旧批次。
	for {
		result, readErr := readLineWithLimit(reader, defaultMaxLineBytes)
		if readErr != nil {
			return errorsx.Wrapf(readErr, "filex.ReadLinesBatch", KindIOError, "stage=read_line path=%s", filePath)
		}
		if result.TooLong {
			return errorsx.Newf(
				"filex.ReadLinesBatch",
				KindLineTooLong,
				"path=%s max=%d",
				filePath,
				defaultMaxLineBytes,
			)
		}

		if result.OK {
			batch = append(batch, result.Line)
		}

		if len(batch) == batchSize {
			if err := handle(batch); err != nil {
				return errorsx.Wrapf(err, "filex.ReadLinesBatch", KindHandleError, "path=%s", filePath)
			}
			batch = make([]string, 0, batchSize)
		}

		if !result.OK {
			break
		}
	}
	if len(batch) > 0 {
		if err := handle(batch); err != nil {
			return errorsx.Wrapf(err, "filex.ReadLinesBatch", KindHandleError, "path=%s", filePath)
		}
	}
	return nil
}

// WriteLines 写入文本行。
// 语义:
// - appendMode=true: 追加写入。
// - appendMode=false: 覆盖写入（先截断再写）。
func WriteLines(filePath string, lines []string, appendMode bool) error {
	// op 是该公开入口写入和关闭阶段统一使用的检索标识。
	const op = "filex.WriteLines"
	if err := ensureParentDir(filePath, op); err != nil {
		return err
	}

	// 打开阶段：覆盖模式明确截断，追加模式保持已有内容。
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	file, err := os.OpenFile(filePath, flag, 0644)
	if err != nil {
		return errorsx.Wrapf(err, op, KindIOError, "stage=open path=%s", filePath)
	}

	// 写入阶段：分两次写 line 和换行，避免构造 line+"\n" 临时字符串。
	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := writer.WriteString(line); err != nil {
			_ = file.Close()
			return errorsx.Wrapf(err, op, KindIOError, "stage=write path=%s", filePath)
		}
		if err := writer.WriteByte('\n'); err != nil {
			_ = file.Close()
			return errorsx.Wrapf(err, op, KindIOError, "stage=write_newline path=%s", filePath)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return errorsx.Wrapf(err, op, KindIOError, "stage=flush path=%s", filePath)
	}
	if err := file.Close(); err != nil {
		return errorsx.Wrapf(err, op, KindIOError, "stage=close path=%s", filePath)
	}
	return nil
}

// WriteBytes 写入字节数组。
// 语义:
// - appendMode=true: 追加写入。
// - appendMode=false: 覆盖写入（先截断再写）。
func WriteBytes(filePath string, data []byte, appendMode bool) error {
	return writeBytes(filePath, data, appendMode, "filex.WriteBytes")
}

// WriteString 写入字符串。
// 语义:
// - appendMode=true: 追加写入。
// - appendMode=false: 覆盖写入（先截断再写）。
func WriteString(filePath string, value string, appendMode bool) error {
	return writeString(filePath, value, appendMode, "filex.WriteString")
}

// RemoveUTF8BOM 去除 UTF-8 BOM 头，返回去除后的字节和是否命中 BOM。
func RemoveUTF8BOM(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, utf8BOM) {
		return data[len(utf8BOM):], true
	}
	return data, false
}

// writeBytes 执行字节入口的写入和关闭流程。
// 阶段角色：由 openWriteFile 统一资源准备，本方法只保留 bytes 写入语义。
func writeBytes(filePath string, data []byte, appendMode bool, op string) error {
	file, err := openWriteFile(filePath, appendMode, op)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errorsx.Wrapf(err, op, KindIOError, "stage=write path=%s", filePath)
	}
	if err := file.Close(); err != nil {
		return errorsx.Wrapf(err, op, KindIOError, "stage=close path=%s", filePath)
	}
	return nil
}

// writeString 执行字符串入口的写入和关闭流程，避免为大字符串分配等长 []byte。
// 阶段角色：保持 WriteString 与 WriteBytes 相同的目录、追加和错误契约。
func writeString(filePath string, value string, appendMode bool, op string) error {
	file, err := openWriteFile(filePath, appendMode, op)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(file, value); err != nil {
		_ = file.Close()
		return errorsx.Wrapf(err, op, KindIOError, "stage=write path=%s", filePath)
	}
	if err := file.Close(); err != nil {
		return errorsx.Wrapf(err, op, KindIOError, "stage=close path=%s", filePath)
	}
	return nil
}

// openWriteFile 创建缺失父目录并按追加或覆盖语义打开文件。
// 阶段角色：统一 bytes/string 写入入口的资源准备和打开错误映射。
func openWriteFile(filePath string, appendMode bool, op string) (*os.File, error) {
	// 准备阶段：写入 API 统一自动创建缺失父目录。
	if err := ensureParentDir(filePath, op); err != nil {
		return nil, err
	}

	// 打开阶段：追加模式保留旧内容，覆盖模式在成功打开时截断。
	flag := os.O_CREATE | os.O_WRONLY
	if appendMode {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	file, err := os.OpenFile(filePath, flag, 0644)
	if err != nil {
		return nil, errorsx.Wrapf(err, op, KindIOError, "stage=open path=%s", filePath)
	}
	return file, nil
}

// ensureParentDir 为写入入口创建缺失父目录，并把失败归属到调用入口 op。
func ensureParentDir(filePath string, op string) error {
	parentDir := filepath.Dir(filePath)
	if parentDir == "." || parentDir == "" {
		return nil
	}
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return errorsx.Wrapf(err, op, KindIOError, "stage=mkdir_parent path=%s", filePath)
	}
	return nil
}

// trimLineEnding 去掉单行末尾的 LF 或 CRLF，不处理内容中的其他空白。
func trimLineEnding(raw string) string {
	withoutLF := strings.TrimSuffix(raw, "\n")
	return strings.TrimSuffix(withoutLF, "\r")
}

// readLineWithLimit 推进一行并限制不含 CRLF 的内容字节数。
// 阶段角色：为全量和批量读行提供相同边界，并区分 EOF、超限与底层 I/O 错误。
func readLineWithLimit(reader *bufio.Reader, maxLineBytes int) (lineReadResult, error) {
	var lineBuffer []byte
	for {
		fragment, readErr := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			// 长度阶段：末尾 CR 暂按潜在 CRLF 处理；若下一片不是 LF，会在下一轮重新计入内容。
			nextLength := len(lineBuffer) + len(fragment)
			hasLF := fragment[len(fragment)-1] == '\n'
			contentLength := nextLength
			if hasLF {
				contentLength = nextLength - 1
				if contentLength > 0 {
					if len(fragment) >= 2 && fragment[len(fragment)-2] == '\r' {
						contentLength--
					} else if len(fragment) == 1 && len(lineBuffer) > 0 && lineBuffer[len(lineBuffer)-1] == '\r' {
						contentLength--
					}
				}
			} else if fragment[len(fragment)-1] == '\r' {
				contentLength--
			}
			if contentLength > maxLineBytes {
				return lineReadResult{TooLong: true}, nil
			}

			// 快路径：单个 reader fragment 已包含完整行时，避免额外 lineBuffer 分配。
			if len(lineBuffer) == 0 && hasLF {
				return lineReadResult{Line: trimLineEnding(string(fragment)), OK: true}, nil
			}
			lineBuffer = append(lineBuffer, fragment...)
			if hasLF {
				return lineReadResult{Line: trimLineEnding(string(lineBuffer)), OK: true}, nil
			}
		}

		// 状态阶段：BufferFull 继续拼接；EOF 以 OK=false 表达终止，其他错误原样交给外层映射。
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			if len(lineBuffer) == 0 {
				return lineReadResult{}, nil
			}
			return lineReadResult{Line: trimLineEnding(string(lineBuffer)), OK: true}, nil
		}
		return lineReadResult{}, readErr
	}
}
