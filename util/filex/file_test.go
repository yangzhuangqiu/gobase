package filex

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// TestReadWriteLines 验证覆盖、追加和读行组合保持文本行顺序。
// 测试方法：先覆盖写 a/b，再追加 c，读取后断言三行。
func TestReadWriteLines(t *testing.T) {
	// 写入阶段：覆盖写建立初始内容，追加写验证不会截断。
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")

	if err := WriteLines(path, []string{"a", "b"}, false); err != nil {
		t.Fatalf("WriteLines error: %v", err)
	}
	if err := WriteLines(path, []string{"c"}, true); err != nil {
		t.Fatalf("WriteLines append error: %v", err)
	}

	lines, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("unexpected lines len: %d", len(lines))
	}
}

// TestReadLinesBatch 验证末批不足 batchSize 时仍会回调。
// 测试方法：按批大小 3 读取四行文件，断言回调两次。
func TestReadLinesBatch(t *testing.T) {
	// 准备阶段：四行数据配合 batchSize=3 形成完整批和尾批。
	dir := t.TempDir()
	path := filepath.Join(dir, "batch.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	batchCount := 0
	err := ReadLinesBatch(path, 3, func(lines []string) error {
		batchCount++
		return nil
	})
	if err != nil {
		t.Fatalf("ReadLinesBatch error: %v", err)
	}
	if batchCount != 2 {
		t.Fatalf("unexpected batch count: %d", batchCount)
	}
}

// TestReadLinesBatchInvalidArguments 验证批大小和回调前置条件统一归入参数错误。
// 测试方法：分别传入零 batchSize 与 nil handle，断言 KindInvalidArgument。
func TestReadLinesBatchInvalidArguments(t *testing.T) {
	if err := ReadLinesBatch("unused", 0, func(lines []string) error { return nil }); err == nil || !errorsx.IsKind(err, KindInvalidArgument) {
		t.Fatalf("expected invalid batch size kind, got=%v", err)
	}
	if err := ReadLinesBatch("unused", 1, nil); err == nil || !errorsx.IsKind(err, KindInvalidArgument) {
		t.Fatalf("expected nil handle kind, got=%v", err)
	}
}

// TestReadLinesBatchHandleError 验证回调错误保留业务 cause 并附加稳定 Kind。
// 测试方法：读取单行文件并让回调返回 sentinel，断言 KindHandleError 和 errors.Is。
func TestReadLinesBatchHandleError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batch-error.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}
	handleErr := errors.New("handle failed")
	err := ReadLinesBatch(path, 1, func(lines []string) error { return handleErr })
	if !errorsx.IsKind(err, KindHandleError) {
		t.Fatalf("expected kind %s, got=%v", KindHandleError, err)
	}
	if !errors.Is(err, handleErr) {
		t.Fatalf("expected handle cause, got=%v", err)
	}
}

// TestWriteBytesAndRemoveUTF8BOM 验证 bytes 写入保持 BOM 且显式 helper 可移除。
// 测试方法：写入 BOM+a，读取原始 bytes 后断言移除结果和命中标记。
func TestWriteBytesAndRemoveUTF8BOM(t *testing.T) {
	// 准备阶段：保留原始 BOM 写入，随后只通过显式 helper 清理。
	dir := t.TempDir()
	path := filepath.Join(dir, "bytes.bin")

	if err := WriteBytes(path, []byte{0xEF, 0xBB, 0xBF, 'a'}, false); err != nil {
		t.Fatalf("WriteBytes error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file error: %v", err)
	}

	clean, ok := RemoveUTF8BOM(raw)
	if !ok {
		t.Fatal("expected BOM to be removed")
	}
	if string(clean) != "a" {
		t.Fatalf("unexpected clean content: %q", string(clean))
	}
}

// TestExistsAndStringReadWrite 验证路径存在判断及 string 覆盖/追加语义。
// 测试方法：写入前后检查 Exists，再读取 string/bytes 并比较完整内容。
func TestExistsAndStringReadWrite(t *testing.T) {
	// 生命周期阶段：从不存在、创建、追加到两种读取入口逐步验证。
	dir := t.TempDir()
	path := filepath.Join(dir, "text.txt")

	exists, err := Exists(path)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Fatal("file should not exist before write")
	}

	if err := WriteString(path, "line-1", false); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}
	if err := WriteString(path, "\nline-2", true); err != nil {
		t.Fatalf("WriteString append error: %v", err)
	}
	content, err := ReadString(path)
	if err != nil {
		t.Fatalf("ReadString error: %v", err)
	}
	if content != "line-1\nline-2" {
		t.Fatalf("ReadString mismatch: %q", content)
	}

	raw, err := ReadBytes(path)
	if err != nil {
		t.Fatalf("ReadBytes error: %v", err)
	}
	if string(raw) != "line-1\nline-2" {
		t.Fatalf("ReadBytes mismatch: %q", string(raw))
	}
}

// TestWriteAutoCreateParentDir 验证写入入口自动创建缺失父目录。
// 测试方法：向两级不存在目录写文本，再读取并比较。
func TestWriteAutoCreateParentDir(t *testing.T) {
	// 执行阶段：目标父目录在调用前不存在，写入入口必须完成创建。
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c.txt")

	if err := WriteString(path, "hello", false); err != nil {
		t.Fatalf("WriteString error: %v", err)
	}

	content, err := ReadString(path)
	if err != nil {
		t.Fatalf("ReadString error: %v", err)
	}
	if content != "hello" {
		t.Fatalf("unexpected content: %q", content)
	}
}

// TestEnsureDir 验证显式目录创建支持多级路径且幂等存在。
// 测试方法：创建嵌套目录后调用 Exists 断言路径可见。
func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x", "y")
	if err := EnsureDir(target); err != nil {
		t.Fatalf("EnsureDir error: %v", err)
	}
	exists, err := Exists(target)
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Fatal("expected ensured directory to exist")
	}
}

// TestReadStringErrorKind 验证文件 I/O Kind 与标准 fs cause 可同时分支。
// 测试方法：读取不存在文件，断言 KindIOError 和 fs.ErrNotExist。
func TestReadStringErrorKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.txt")
	_, err := ReadString(path)
	if err == nil {
		t.Fatal("expected read error")
	}
	if !errorsx.IsKind(err, KindIOError) {
		t.Fatalf("expected kind %s, got err=%v", KindIOError, err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fs.ErrNotExist in error chain, got err=%v", err)
	}
}

// TestReadLinesMaxLineBoundary 验证内容恰好达到 1 MiB 时允许读取。
// 测试方法：写入最大长度加 LF，断言返回一行完整内容。
func TestReadLinesMaxLineBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "max-line.txt")
	maxLine := strings.Repeat("a", 1*1024*1024)
	if err := os.WriteFile(path, []byte(maxLine+"\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	lines, err := ReadLines(path)
	if err != nil {
		t.Fatalf("ReadLines error: %v", err)
	}
	if len(lines) != 1 || lines[0] != maxLine {
		t.Fatalf("unexpected lines result len=%d", len(lines))
	}
}

// TestReadLineWithLimitAllowsSplitCRLF 验证 CR 与 LF 跨 reader buffer 时不被误计入内容上限。
// 测试方法：让 65535-byte 内容加 CR 恰好填满 buffer，再断言下一片 LF 可完成读取。
func TestReadLineWithLimitAllowsSplitCRLF(t *testing.T) {
	// maxLineBytes 让内容加 CR 恰好填满测试 reader buffer（单位：byte）。
	const maxLineBytes = 65535
	raw := strings.Repeat("a", maxLineBytes) + "\r\n"
	reader := bufio.NewReaderSize(strings.NewReader(raw), maxLineBytes+1)
	result, err := readLineWithLimit(reader, maxLineBytes)
	if err != nil {
		t.Fatalf("readLineWithLimit error: %v", err)
	}
	if result.TooLong || !result.OK || len(result.Line) != maxLineBytes {
		t.Fatalf("unexpected result: ok=%v tooLong=%v len=%d", result.OK, result.TooLong, len(result.Line))
	}
}

// TestReadLinesLongLine 验证超过默认单行上限时稳定拒绝文件。
// 测试方法：写入 2 MiB 行，断言返回 KindLineTooLong。
func TestReadLinesLongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long-line.txt")
	longLine := strings.Repeat("a", 2*1024*1024)
	if err := os.WriteFile(path, []byte(longLine+"\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	_, err := ReadLines(path)
	if err == nil {
		t.Fatal("expected long line error")
	}
	if !errorsx.IsKind(err, KindLineTooLong) {
		t.Fatalf("expected kind %s, got err=%v", KindLineTooLong, err)
	}
}

// TestReadLinesBatchLongLine 验证批量入口应用与全量入口相同的行上限。
// 测试方法：批量读取 2 MiB 行，断言回调前返回 KindLineTooLong。
func TestReadLinesBatchLongLine(t *testing.T) {
	// 准备阶段：超长首行应在任何 batch callback 执行前失败。
	path := filepath.Join(t.TempDir(), "batch-max-line.txt")
	longLine := strings.Repeat("a", 2*1024*1024)
	if err := os.WriteFile(path, []byte(longLine+"\n"), 0644); err != nil {
		t.Fatalf("write file error: %v", err)
	}

	err := ReadLinesBatch(path, 5, func(lines []string) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected long line error")
	}
	if !errorsx.IsKind(err, KindLineTooLong) {
		t.Fatalf("expected kind %s, got err=%v", KindLineTooLong, err)
	}
}
