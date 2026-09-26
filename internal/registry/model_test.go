package registry

import "testing"

func TestNormalizeEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "adds HTTPS", input: "harbor.example.com", want: "https://harbor.example.com"},
		{name: "keeps local HTTP", input: "http://127.0.0.1:5000/", want: "http://127.0.0.1:5000"},
		{name: "rejects path", input: "https://harbor.example.com/api", wantErr: true},
		{name: "rejects credentials", input: "https://user:token@harbor.example.com", wantErr: true},
		{name: "rejects other scheme", input: "ftp://harbor.example.com", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeEndpoint(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize endpoint: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}
