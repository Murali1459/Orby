APP      := orby
LDFLAGS  := -s -w
BUILD    := dist

.PHONY: all clean linux-amd64 linux-arm64

all: linux-amd64 linux-arm64

linux-amd64:
	GOOS=linux GOARCH=amd64 go build -tags embed -ldflags "$(LDFLAGS)" -o $(BUILD)/$(APP)-linux-amd64 .

linux-arm64:
	GOOS=linux GOARCH=arm64 go build -tags embed -ldflags "$(LDFLAGS)" -o $(BUILD)/$(APP)-linux-arm64 .

clean:
	rm -rf $(BUILD)
