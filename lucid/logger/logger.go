// Package logger re-exports GORM's logger types under the Nimbus lucid module.
package logger

import gormlogger "gorm.io/gorm/logger"

// Interface is the GORM logger contract.
type Interface = gormlogger.Interface

// LogLevel controls verbosity.
type LogLevel = gormlogger.LogLevel

// Default is the default GORM logger instance.
var Default = gormlogger.Default

// Log levels (same semantics as gorm.io/gorm/logger).
const (
	Silent LogLevel = gormlogger.Silent
	Error  LogLevel = gormlogger.Error
	Warn   LogLevel = gormlogger.Warn
	Info   LogLevel = gormlogger.Info
)
