package tools

import (
	"bytes"
	"net/http"
	"os"
	"path"
	"strings"
)

//判断文件是否为二进制文件

var binaryExts = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
	".pdf": true, ".doc": true, ".docx": true,
	".ttf": true, ".woff": true, ".woff2": true, ".eot": true,
	".lockb": true,
}

func IsBinaryFile(filePath string) (bool, string, error) {
	//先判断文件扩展名
	ext := strings.ToLower(path.Ext(filePath))
	if binaryExts[ext] {
		return true, ext, nil
	}
	//内容嗅探
	f, err := os.Open(filePath)
	if err != nil {
		return false, "", err
	}
	defer f.Close()
	//0x00检测
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if n == 0 {
		return false, "empty file", nil
	}
	buf = buf[:n]
	if bytes.Contains(buf, []byte{0}) {
		return true, "contains null byte", nil
	}
	//http.DetectContentType检测
	contentType := http.DetectContentType(buf)
	binaryMimes := []string{
		"image/", "audio/", "video/", "application/octet-stream",
		"application/pdf", "application/zip", "application/gzip",
		"application/x-tar", "application/x-rar-compressed",
		"application/x-executable", "application/x-sharedlib",
		"application/x-mach-binary", "application/wasm",
	}
	for _, mime := range binaryMimes {
		if strings.HasPrefix(contentType, mime) {
			return true, contentType, nil
		}
	}
	//返回
	return false, ext, nil
}
