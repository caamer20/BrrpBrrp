package app

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LoadEnvFile reads literal BRRP_ settings without expanding shell expressions.
// Explicit environment values take precedence over the file.
func LoadEnvFile(path string) error {
	values, err := ReadEnvFile(path)
	if err != nil {
		return err
	}
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return err
			}
		}
	}
	return nil
}

// ReadEnvFile inspects settings without changing the current process environment.
func ReadEnvFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !strings.HasPrefix(key, "BRRP_") || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("invalid setting on line %d", line)
		}
		if strings.HasPrefix(value, `"`) {
			var err error
			value, err = strconv.Unquote(value)
			if err != nil {
				return nil, fmt.Errorf("invalid quoted value on line %d", line)
			}
		} else if strings.HasPrefix(value, "'") {
			if len(value) < 2 || !strings.HasSuffix(value, "'") {
				return nil, fmt.Errorf("invalid quoted value on line %d", line)
			}
			value = value[1 : len(value)-1]
		}
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return values, scanner.Err()
}
