package logger

import (
	"fmt"
	"log"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Initialize the logger
// https://dev.to/ronnymedina/golang-logging-configuration-with-zap-practical-implementation-tips-6g7
func InitLogger(serverId int) *zap.SugaredLogger {

	fileName := fmt.Sprintf("server_%d.log", serverId)
	logFile, err := os.OpenFile(fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open lof file %s %v", fileName, err)
	}

	config := zap.NewProductionEncoderConfig()
	config.EncodeTime = zapcore.ISO8601TimeEncoder
	encoder := zapcore.NewJSONEncoder(config)
	fileSync := zapcore.AddSync(logFile)

	core := zapcore.NewCore(encoder, fileSync, zap.DebugLevel)

	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.Fields(zap.Int("serverID", serverId)))

	return logger.Sugar()
}
