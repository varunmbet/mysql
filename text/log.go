/*
日志库
@author : hyperion
@since  : 2017-3-8
@version: 1.0
*/
package Text

import (
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	LogList  map[string]*Logger = nil //Log对象列表
	LogPath  string             = ""  //日志存放路径，为空时使用 程序目录/log/
	LT_DAY   int                = 1
	LT_MONTH int                = 2

	logMu sync.Mutex
)

type (
	Logger struct {
		Dir      string
		FilePath string
		Type     int
		Flags    int
	}
)

// log日志
func Log(filename ...string) *Logger {
	var fname string = "app"
	if len(filename) > 0 {
		fname = filename[0]
	}
	logMu.Lock()
	defer logMu.Unlock()
	if LogList == nil {
		LogList = make(map[string]*Logger, 10)
	}
	if l, ok := LogList[fname]; ok {
		return l
	}
	l := &Logger{Dir: LogPath}
	if l.Dir == "" {
		if appDir := GetAppDir(); appDir != "" {
			l.Dir = filepath.Join(appDir, "log")
		} else {
			l.Dir = filepath.Join(os.TempDir(), "log")
		}
	}
	if !IsExists(l.Dir) { //如果目录不存在，则创建此目录
		CreateDir(l.Dir)
	}
	l.FilePath = filepath.Join(l.Dir, fname+".log")
	LogList[fname] = l
	return l
}

// 添加log日志。logtype-->默认（0）：信息日志；1：警告日志；2：错误日志；3：崩溃日志；4：调试日志；
// 只写本日志文件，不改动标准库 log 包的全局输出；文件打不开时返回错误。
func (l *Logger) Add(content string, logtype int) error {
	return l.add(content, logtype, l.Flags)
}

func (l *Logger) add(content string, logtype int, flags int) error {
	var infoTag = "[INFO]"
	if logtype == 1 {
		infoTag = "[WARNING]"
	} else if logtype == 2 {
		infoTag = "[ERROR]"
	} else if logtype == 3 {
		infoTag = "[FATAL]"
	} else if logtype == 4 {
		infoTag = "[DEBUG]"
	}
	if flags == 0 {
		flags = log.LstdFlags
	}
	logFile, err := os.OpenFile(l.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer logFile.Close()
	return log.New(logFile, infoTag, flags).Output(3, content)
}

// 普通信息
func (l *Logger) Info(content string) error {
	return l.add(content, 0, 0)
}

// 警告信息
func (l *Logger) Warning(content string) error {
	return l.add(content, 1, log.Lshortfile|log.LstdFlags)
}

// 错误信息
func (l *Logger) Error(content string) error {
	return l.add(content, 2, log.Lshortfile|log.LstdFlags)
}

// 严重错误
func (l *Logger) Fatal(content string) error {
	return l.add(content, 3, log.Lshortfile|log.LstdFlags)
}

// 调试信息
func (l *Logger) Debug(content string) error {
	return l.add(content, 4, log.Lshortfile|log.LstdFlags)
}
