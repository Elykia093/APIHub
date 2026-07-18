package main

import "testing"

func TestHealthcheckTargetMatchesServiceHostAndPort(t *testing.T) {
	for _, test := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"scientific IPv6", map[string]string{"HOST": "::1", "PORT": "1e3"}, "http://[::1]:1000/health/ready"},
		{"decimal wildcard IPv6", map[string]string{"HOST": "::", "PORT": "4180.0"}, "http://[::1]:4180/health/ready"},
		{"wildcard IPv4", map[string]string{"HOST": "0.0.0.0"}, "http://127.0.0.1:4180/health/ready"},
	} {
		t.Run(test.name, func(t *testing.T) {
			target, err := healthcheckTarget(func(key string) (string, bool) {
				value, ok := test.env[key]
				return value, ok
			})
			if err != nil || target != test.want {
				t.Fatalf("healthcheckTarget() = %q, %v; want %q", target, err, test.want)
			}
		})
	}
}
