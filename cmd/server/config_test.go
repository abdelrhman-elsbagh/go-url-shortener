package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate(t *testing.T) {
	valid := config{port: 8080, dbPath: "./data/urls.db", rlRPS: 10, rlBurst: 20}

	tests := []struct {
		name    string
		cfg     config
		wantErr bool
	}{
		{"valid", valid, false},
		{"port zero", config{port: 0, dbPath: "./data", rlRPS: 10, rlBurst: 20}, true},
		{"port too high", config{port: 99999, dbPath: "./data", rlRPS: 10, rlBurst: 20}, true},
		{"empty dbPath", config{port: 8080, dbPath: "", rlRPS: 10, rlBurst: 20}, true},
		{"rps zero", config{port: 8080, dbPath: "./data", rlRPS: 0, rlBurst: 0}, true},
		{"rps negative", config{port: 8080, dbPath: "./data", rlRPS: -1, rlBurst: 5}, true},
		{"burst less than rps", config{port: 8080, dbPath: "./data", rlRPS: 10, rlBurst: 5}, true},
		{"burst equal to rps is ok", config{port: 8080, dbPath: "./data", rlRPS: 10, rlBurst: 10}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
