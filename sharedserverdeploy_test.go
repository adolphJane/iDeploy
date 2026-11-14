package main

import (
	"strings"
	"testing"
)

func TestSharedServerTLSAdviceDistinguishesCertificateFormatAndHost(t *testing.T) {
	format := sharedServerTLSAdvice(`x509: "MySQL_Server_5.7.42_Auto_Generated_Server_Certificate" certificate is not standards compliant`)
	if !strings.Contains(format, "更换连接域名不能修复") {
		t.Fatalf("unexpected advice for malformed certificate: %s", format)
	}
	host := sharedServerTLSAdvice("x509: cannot validate certificate for 223.254.144.164 because it doesn't contain any IP SANs")
	if !strings.Contains(host, "证书未覆盖当前连接地址") {
		t.Fatalf("unexpected advice for host mismatch: %s", host)
	}
}
