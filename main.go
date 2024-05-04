package main;

import (
	"log/slog"

	"net/http"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"errors"
	"strings"

	"github.com/samott/crash-backend/game"
	"github.com/samott/crash-backend/bank"

	"database/sql"

	"github.com/spruceid/siwe-go"

	"github.com/go-sql-driver/mysql"
	"github.com/zishang520/socket.io/v2/socket"

	"github.com/shopspring/decimal"
);

var currencies map[string]bool;

type AuthParams struct {
	message string;
	signature string;
}

type PlaceBetParams struct {
	betAmount decimal.Decimal;
	autoCashOut decimal.Decimal;
	currency string;
}

type Session struct {
	wallet string;
}

func validateToken(token string, session *Session) error {
	if token != "token123" {
		return errors.New("Invalid token");
	}

	session.wallet = "0x1111111111111111111111111111111111111111";

	return nil;
}

func authenticateUser(payload string, signature string) (string, error) {
	message, err := siwe.ParseMessage(payload);

	if err != nil {
		return "", err;
	}

	publicKey, err := message.VerifyEIP191(signature);

	if err != nil {
		return "", err;
	}

	bytes := crypto.FromECDSAPub(publicKey);
	wallet := hexutil.Encode(bytes);

	return wallet, nil;
}

func generateToken(wallet string) (string) {
	if len(wallet) == 0 {
		return "";
	}

	return "token123";
}

func validateAuthenticateParams(result *AuthParams, data ...any) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, errors.New("Invalid parameters");
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, errors.New("Invalid parameters");
	}

	message, ok1 := params["message"].(string);
	signature, ok2 := params["signature"].(string);

	if !ok1 || !ok2 {
		return nil, errors.New("Invalid parameters");
	}

	*result = AuthParams{
		message: message,
		signature: signature,
	};

	if len(data) != 2 {
		return nil, nil;
	}

	callback, ok := data[1].(func([]any, error));

	if !ok {
		// Ought to be an error, but we'll treat it
		// as if there were no callback supplied
		return nil, nil;
	}

	return callback, nil;
}

func validatePlaceBetParams(result *PlaceBetParams, data ...any) (func([]any, error), error) {
	if len(data) == 0 {
		return nil, errors.New("Invalid parameters");
	}

	params, ok := data[0].(map[string]any);

	if !ok {
		return nil, errors.New("Invalid parameters");
	}

	betAmountStr, ok1 := params["betAmount"].(string);
	autoCashOutStr, ok2 := params["autoCashOut"].(string);
	currency, ok3 := params["currency"].(string);

	if !ok1 || !ok2 || !ok3 {
		return nil, errors.New("Invalid parameters");
	}

	betAmount, err1 := decimal.NewFromString(betAmountStr);
	autoCashOut, err2 := decimal.NewFromString(autoCashOutStr);

	if err1 != nil || err2 != nil {
		return nil, errors.New("Invalid decimal numbers");
	}

	if _, ok := currencies[currency]; !ok {
		return nil, errors.New("Unsupported currency");
	}

	*result = PlaceBetParams{
		betAmount: betAmount,
		autoCashOut: autoCashOut,
		currency: currency,
	};

	if len(data) != 2 {
		return nil, nil;
	}

	callback, ok := data[1].(func([]any, error));

	if !ok {
		// Ought to be an error, but we'll treat it
		// as if there were no callback supplied
		return nil, nil;
	}

	return callback, nil;
}

func main() {
	slog.Info("Crash running...");

	currencies = map[string]bool{
		"eth": true,
	};

	dbConfig := mysql.Config{
		User: "crash",
		DBName: "crash",
		Addr: "localhost",
		AllowNativePasswords: true,
	};

	var db, err = sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		return;
	}

	defer db.Close();

	io := socket.NewServer(nil, nil);
	bankObj, err := bank.NewBank(db);

	if err != nil {
		slog.Error("Failed to init bank");
		return;
	}

	gameObj, err := game.NewGame(io, db, game.Bank(bankObj));

	if err != nil {
		slog.Error("Failed to init game");
		return;
	}

	http.Handle("/socket.io/", io.ServeHandler(nil));
	go http.ListenAndServe(":4000", nil);

	io.On("connection", func(clients ...any) {
		client := clients[0].(*socket.Socket);

		slog.Info("Client connected", "clientId", client.Id);

		headers := client.Handshake().Headers;

		header, headerFound := headers["Authorization"];

		if !headerFound {
			gameObj.HandleConnect(client, "");

			client.On("authenticate", func(data ...any) {
				slog.Info("Client authenticating", "client", client.Id);

				var params AuthParams;

				callback, err := validateAuthenticateParams(&params, data...);

				if err != nil {
					slog.Warn("Invalid parameters", "client", client.Id);
					client.Disconnect(true);
					return;
				}

				wallet, err := authenticateUser(params.message, params.signature);

				if err != nil {
					slog.Warn("Invalid signature", "client", client.Id);
					client.Emit("authenticate", map[string]any{
						"success": false,
					});
					return;
				}

				token := generateToken(wallet);

				slog.Info("Authentication successful", "client", client.Id);

				if callback != nil {
					callback(
						[]any{ map[string]any{
							"token": token,
							"success": true,
						} },
						nil,
					);
				}
			});

			client.On("disconnected", func(...any) {
				slog.Info("Client disconnected", "client", client);
				gameObj.HandleDisconnect(client);
			});

			return;
		}

 		if len(header) == 0 || !strings.HasPrefix(header[0], "Bearer ") {
			slog.Warn("Missing auth header");
			client.Disconnect(true);
			return;
		}

		token := strings.TrimPrefix(header[0], "Bearer ");

		var session Session;

		if err := validateToken(token, &session); err != nil {
			slog.Warn("Invalid session");
			client.Disconnect(true);
			return;
		}

		slog.Info("User authenticated", "wallet", session.wallet);

		gameObj.HandleConnect(client, session.wallet);

		client.On("placeBet", func(data ...any) {
			slog.Info("PlaceBet for user", "wallet", session.wallet);

			var params PlaceBetParams;

			callback, err := validatePlaceBetParams(&params, data...);

			if err != nil {
				slog.Warn("Invalid parameters", "client", client.Id);
				client.Disconnect(true);
				return;
			}

			err = gameObj.HandlePlaceBet(
				client,
				session.wallet,
				params.currency,
				params.betAmount,
				params.autoCashOut,
			);

			if callback != nil {
				callback(
					[]any{ map[string]any{
						"success": err == nil,
					} },
					nil,
				);
			}
		});

		client.On("cashOut", func(...any) {
			slog.Info("CashOut for user", "wallet", session.wallet);
			gameObj.HandleCashOut(session.wallet);
		});

		client.On("disconnected", func(...any) {
			slog.Info("Client disconnected", "client", client);
			gameObj.HandleDisconnect(client);
		});
	});

	exit := make(chan struct{});
	<- exit;
}
