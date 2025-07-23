package mysql

import (
	"os"
	"time"
)

type sqllog struct {
	_StartTime   time.Time
	_ExecuteTime time.Duration
	_logger      *SimpleLogger
	_sb          *SQLBuilder
}

func NewSqlLog(sb *SQLBuilder) *sqllog {
	log := &sqllog{_sb: sb, _StartTime: time.Now()}
	logger := NewSimpleLogger(os.Stdout)
	logger.SetLevel(0)
	log.SetLogger(logger)
	return log
}

func (l *sqllog) Start() {
	l._StartTime = time.Now()
}

func (l *sqllog) SetLogger(logger *SimpleLogger) {
	l._logger = logger
}

func (l *sqllog) ShowLog() {
	if l._sb._Debug {
		l._ExecuteTime = time.Now().Sub(l._StartTime)
		if l._ExecuteTime > 0 {
			l._logger.Infof("[SQL]%s %v - %v", l._sb._SQL, l._sb._param, l._ExecuteTime)
		} else {
			l._logger.Infof("[SQL]%s %v ", l._sb._SQL, l._sb._param)
		}
	}
}
