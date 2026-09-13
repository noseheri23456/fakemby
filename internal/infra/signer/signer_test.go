package signer

import (
	"testing"
	"time"
)

func TestNewDisabledWhenKeyEmpty(t *testing.T) {
	if s := New("", 3600); s != nil {
		t.Fatal("密钥为空时应返回 nil（表示禁用签名），而不是可用的签名器")
	}
}

func TestNewFallsBackToDefaultTTL(t *testing.T) {
	s := New("k", 0)
	if s == nil {
		t.Fatal("密钥非空时应返回可用签名器")
	}
	if s.ttl != time.Hour {
		t.Fatalf("TTL 非法时应回落 1 小时，实际 %v", s.ttl)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := New("secret", 3600)
	p := Payload{MediaType: "video", ItemID: "item-1", SourceID: "src-1", UserID: "user-1"}

	exp, sig, err := s.Sign(p)
	if err != nil {
		t.Fatalf("Sign 失败: %v", err)
	}
	if exp <= time.Now().Unix() {
		t.Fatalf("过期时间应在未来: %d", exp)
	}
	if err := s.Verify(p, exp, sig); err != nil {
		t.Fatalf("同一 payload 应校验通过: %v", err)
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	s := New("secret", 3600)
	_, sig, _ := s.Sign(Payload{MediaType: "video", ItemID: "item-1", SourceID: "src-1", UserID: "user-1"})

	// 攻击者把 item 换成别人的
	tampered := Payload{MediaType: "video", ItemID: "item-2", SourceID: "src-1", UserID: "user-1"}
	exp := time.Now().Add(time.Hour).Unix()
	if err := s.Verify(tampered, exp, sig); err != ErrBadSignature {
		t.Fatalf("篡改 payload 应被拒绝，实际: %v", err)
	}
}

func TestVerifyRejectsTamperedUser(t *testing.T) {
	s := New("secret", 3600)
	_, sig, _ := s.Sign(Payload{MediaType: "video", ItemID: "item-1", SourceID: "src-1", UserID: "user-1"})

	// 换 uid 也应该失败：签名覆盖了 UserID 维度
	tampered := Payload{MediaType: "video", ItemID: "item-1", SourceID: "src-1", UserID: "user-2"}
	if err := s.Verify(tampered, time.Now().Add(time.Hour).Unix(), sig); err != ErrBadSignature {
		t.Fatalf("换 uid 应被拒绝，实际: %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s := New("secret", 60)
	p := Payload{MediaType: "video", ItemID: "i", SourceID: "s", UserID: "u"}

	// 直接构造一个已过期的时间戳（不依赖 sleep）
	expired := time.Now().Add(-time.Minute).Unix()
	sig := s.sign(p.String(), expired)

	if err := s.Verify(p, expired, sig); err != ErrExpired {
		t.Fatalf("过期签名应返回 ErrExpired，实际: %v", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	a := New("key-a", 3600)
	b := New("key-b", 3600)

	p := Payload{MediaType: "video", ItemID: "i", SourceID: "s", UserID: "u"}
	exp, sig, _ := a.Sign(p)
	if err := b.Verify(p, exp, sig); err != ErrBadSignature {
		t.Fatalf("换密钥后应校验失败，实际: %v", err)
	}
}

func TestVerifyRejectsEmptyInputs(t *testing.T) {
	s := New("secret", 3600)
	p := Payload{ItemID: "i"}
	if err := s.Verify(p, 0, "abc"); err != ErrBadSignature {
		t.Fatalf("exp=0 应被拒绝，实际: %v", err)
	}
	if err := s.Verify(p, time.Now().Add(time.Hour).Unix(), ""); err != ErrBadSignature {
		t.Fatalf("sig 为空应被拒绝，实际: %v", err)
	}
}

func TestVerifyRawDisabledSigner(t *testing.T) {
	var s *Signer // nil = 禁用
	if err := s.VerifyRaw("x", time.Now().Unix(), "y"); err == nil {
		t.Fatal("签名器禁用时校验必须失败，否则等于放行")
	}
}

// TestPayloadFormatIsStable 钉住 payload 的序列化格式。
// 字段顺序或分隔符变了，此前签发的全部直链立即失效——这是破坏性变更，必须有测试盯着。
func TestPayloadFormatIsStable(t *testing.T) {
	p := Payload{MediaType: "video", ItemID: "i1", SourceID: "s1", UserID: "u1", ExtraIndex: "2"}
	want := "video|i1|s1|u1|2"
	if got := p.String(); got != want {
		t.Fatalf("payload 格式漂移: want %q, got %q", want, got)
	}
}

func TestSignIsDeterministicForSameExp(t *testing.T) {
	s := New("secret", 3600)
	p := Payload{MediaType: "video", ItemID: "i", SourceID: "s", UserID: "u"}
	exp := int64(1_800_000_000)
	// 注意：不能写成 s.sign(...) != s.sign(...)，左右表达式字面上相同时
	// staticcheck 会判为恒 false（SA4000），断言就空转了——必须先落到变量里。
	first := s.sign(p.String(), exp)
	second := s.sign(p.String(), exp)
	if first != second {
		t.Fatal("同一 payload + 同一 exp 应得到相同签名")
	}
	if first == s.sign(p.String(), exp+1) {
		t.Fatal("exp 参与签名，不同 exp 应得到不同签名")
	}
}
