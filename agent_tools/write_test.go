package agent_tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yinxiangpingfan/cc-mini-go/tools"
)

// resetReadFiles 每个测试前清空 ReadFiles 避免互相干扰
func resetReadFiles(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for k := range ReadFiles {
			delete(ReadFiles, k)
		}
	})
}

func TestWriteFile_NewFile(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "new.txt")

	err := writeFile(f, "hello world\n")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// 验证文件内容
	data, _ := os.ReadFile(f)
	if string(data) != "hello world\n" {
		t.Fatalf("expected 'hello world\\n', got: %s", string(data))
	}
	// 验证 hash 已记录
	if _, ok := ReadFiles[f]; !ok {
		t.Fatal("expected file to be recorded in ReadFiles after write")
	}
}

func TestWriteFile_NewFileWithNestedDir(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "a", "b", "c", "deep.txt")

	err := writeFile(f, "nested content")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "nested content" {
		t.Fatalf("expected 'nested content', got: %s", string(data))
	}
}

func TestWriteFile_OverwriteWithoutRead(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "existing.txt")
	os.WriteFile(f, []byte("original"), 0644)

	// 没有读过，直接写应该报错
	err := writeFile(f, "overwrite attempt")
	if err == nil {
		t.Fatal("expected error when overwriting without reading first")
	}
	if !strings.Contains(err.Error(), "must be read before overwriting") {
		t.Fatalf("expected 'must be read before overwriting' error, got: %v", err)
	}
	// 验证原文件未被修改
	data, _ := os.ReadFile(f)
	if string(data) != "original" {
		t.Fatalf("expected file unchanged, got: %s", string(data))
	}
}

func TestWriteFile_ReadThenOverwrite(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "readfirst.txt")
	os.WriteFile(f, []byte("old content"), 0644)

	// 模拟读取：计算 hash 并记录
	hash, err := tools.HashFile(f)
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	ReadFiles[f] = hash

	// 现在覆盖写入应该成功
	err = writeFile(f, "new content")
	if err != nil {
		t.Fatalf("expected no error after read, got: %v", err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got: %s", string(data))
	}
}

func TestWriteFile_FileModifiedAfterRead(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "modified.txt")
	os.WriteFile(f, []byte("version1"), 0644)

	// 读取并记录 hash
	hash, _ := tools.HashFile(f)
	ReadFiles[f] = hash

	// 外部修改文件（模拟其他进程修改）
	os.WriteFile(f, []byte("version2 by external"), 0644)

	// 写入应该失败：hash 不一致
	err := writeFile(f, "my overwrite")
	if err == nil {
		t.Fatal("expected error when file modified since last read")
	}
	if !strings.Contains(err.Error(), "has been modified since last read") {
		t.Fatalf("expected 'modified since last read' error, got: %v", err)
	}
	// 验证文件未被覆盖
	data, _ := os.ReadFile(f)
	if string(data) != "version2 by external" {
		t.Fatalf("expected file unchanged after rejected write, got: %s", string(data))
	}
}

func TestWriteFile_WriteThenOverwriteAgain(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "twice.txt")

	// 第一次写入新文件
	err := writeFile(f, "first write")
	if err != nil {
		t.Fatalf("first write error: %v", err)
	}

	// 第二次覆盖同一文件（因为第一次写入后 hash 已更新）
	err = writeFile(f, "second write")
	if err != nil {
		t.Fatalf("second write should succeed since hash was updated, got: %v", err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "second write" {
		t.Fatalf("expected 'second write', got: %s", string(data))
	}
}

func TestWriteFile_EmptyContent(t *testing.T) {
	resetReadFiles(t)
	tmp := t.TempDir()
	f := filepath.Join(tmp, "empty.txt")

	err := writeFile(f, "")
	if err != nil {
		t.Fatalf("expected no error writing empty content, got: %v", err)
	}
	data, _ := os.ReadFile(f)
	if string(data) != "" {
		t.Fatalf("expected empty file, got: %s", string(data))
	}
}
