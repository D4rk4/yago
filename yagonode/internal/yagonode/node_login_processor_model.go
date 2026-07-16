package yagonode

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

const (
	maximumProcessorInformationBytes = 64 << 10
	maximumProcessorInformationLine  = 8 << 10
	maximumProcessorModelRunes       = 128
)

func linuxProcessorModel() (string, error) {
	file, err := os.Open("/proc/cpuinfo")
	return processorModelFromOpenedInformation(file, err)
}

func processorModelFromOpenedInformation(
	reader io.ReadCloser,
	openErr error,
) (string, error) {
	if openErr != nil {
		return "", fmt.Errorf("open processor information: %w", openErr)
	}
	model, scanErr := processorModelFromReader(
		io.LimitReader(reader, maximumProcessorInformationBytes),
	)
	closeErr := reader.Close()
	if scanErr != nil {
		return "", scanErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("close processor information: %w", closeErr)
	}

	return model, nil
}

func processorModelFromReader(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), maximumProcessorInformationLine)
	processor := ""
	hardware := ""
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		value = normalizedProcessorModel(value)
		if value == "" {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name":
			return value, nil
		case "Processor":
			if processor == "" {
				processor = value
			}
		case "Hardware":
			if hardware == "" {
				hardware = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan processor information: %w", err)
	}
	if processor != "" {
		return processor, nil
	}

	return hardware, nil
}

func normalizedProcessorModel(value string) string {
	value = strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return ' '
		}
		return value
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > maximumProcessorModelRunes {
		value = string(runes[:maximumProcessorModelRunes])
	}

	return value
}
