package main;

import (
	"log/slog"
	"github.com/samott/crash-backend/game"
	"github.com/zishang520/socket.io/v2/socket"
);

func authenticateHandler(
	client *socket.Socket,
	_ game.Game /* gameObj */,
	data ...any,
) {
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

	token, err := generateToken(wallet);

	if err != nil {
		slog.Error("Error generating token", "err", err);
		client.Emit("authenticate", map[string]any{
			"success": false,
		});
		return;
	}

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
}

func disconnectedHandler(
	client *socket.Socket,
	gameObj game.Game,
	_ ...any /* data */,
) {
	slog.Info("Client disconnected", "client", client);
	gameObj.HandleDisconnect(client);
};

func refreshTokenHandler(
	client *socket.Socket,
	session Session,
	_ game.Game /* gameObj */,
	data ...any,
) {
	slog.Info("Refreshing JWT token", "wallet", session.wallet);

	token, err := generateToken(session.wallet);

	if err != nil {
		slog.Error("Error generating token", "err", err);
		client.Emit("authenticate", map[string]any{
			"success": false,
		});
		return;
	}

	callback := extractCallback(0, data...);

	if callback != nil {
		callback(
			[]any{ map[string]any{
				"token": token,
				"success": true,
			} },
			nil,
		);
	}
}

func placeBetHandler(
	client *socket.Socket,
	session Session,
	gameObj game.Game,
	data ...any,
) {
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
}

func cancelBetHandler(
	_ *socket.Socket /* client */,
	session Session,
	gameObj game.Game,
	data ...any,
) {
	slog.Info("CancelBet for user", "wallet", session.wallet);

	err := gameObj.HandleCancelBet(session.wallet);

	callback := extractCallback(0, data...);

	if callback != nil {
		callback(
			[]any{ map[string]any{
				"success": err == nil,
			} },
			nil,
		);
	}
}

func cashOutHandler(
	_ *socket.Socket /* client */,
	session Session,
	gameObj game.Game,
	_ ...any /* data */,
) {
	slog.Info("CashOut for user", "wallet", session.wallet);
	gameObj.HandleCashOut(session.wallet);
}
