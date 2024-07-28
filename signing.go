package main

import (
	"fmt"
	"crypto/ecdsa"
	"github.com/shopspring/decimal"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/ethereum/go-ethereum/common/math"
)

var types = apitypes.Types{
	"EIP712Domain": {
		{ Name: "name", Type: "string" },
		{ Name: "version", Type: "string" },
		{ Name: "chainId", Type: "uint256" },
		{ Name: "verifyingContract", Type: "address" },
	},
	"WithdrawalRequest": {
		{ Name: "user", Type: "address" },
		{ Name: "coinId", Type: "uint32" },
		{ Name: "amount", Type: "uint256" },
		{ Name: "nonce", Type: "uint256" },
		{ Name: "tasks", Type: "Task[]" },
	},
	"Task": {
		{ Name: "taskType", Type: "uint8" },
		{ Name: "user", Type: "address" },
		{ Name: "coinId", Type: "uint32" },
		{ Name: "amount", Type: "uint256" },
		{ Name: "nonce", Type: "uint256" },
	},
}

func createWithdrawalRequest(
	wallet string,
	amount decimal.Decimal,
	currency string,
) {
	domain := apitypes.TypedDataDomain{
		Name:              "Crash",
		Version:           "1.0",
		ChainId:           math.NewHexOrDecimal256(1),
		VerifyingContract: "0x1111111111111111111111111111111111111111",
	}

	message := apitypes.TypedDataMessage{
		"user":   "0x2222222222222222222222222222222222222222",
		"coinId": 1,
		"amount": "100",
		"nonce":  0,
		"tasks":  []map[string]any{},
	};

	typedData := apitypes.TypedData{
		Types:       types,
		PrimaryType: "WithdrawalRequest",
		Domain:      domain,
		Message:     message,
	}

	signTypedData(typedData);
}

func signTypedData(data apitypes.TypedData) ([]byte, error) {
	var privateKey *ecdsa.PrivateKey = &ecdsa.PrivateKey{};

	domainSeparator, err := data.HashStruct("EIP712Domain", data.Domain.Map());

	if err != nil {
		return nil, err
	}

	typedDataHash, err := data.HashStruct(data.PrimaryType, data.Message);

	if err != nil {
		return nil, err
	}

	rawData := []byte(fmt.Sprintf("\x19\x01%s%s", string(domainSeparator), string(typedDataHash)))

	sighash := crypto.Keccak256(rawData)

	signature, err := crypto.Sign(sighash, privateKey)

	if err != nil {
		return nil, err;
	}

	if signature[64] == 0 || signature[64] == 1 {
		signature[64] += 27
	}

	return signature, nil;
}
