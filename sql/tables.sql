DROP TABLE IF EXISTS `users`;
DROP TABLE IF EXISTS `games`;
DROP TABLE IF EXISTS `bets`;

CREATE TABLE `users` (
	`wallet` uuid PRIMARY KEY NOT NULL,
	`created` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), 
);

CREATE TABLE `games` (
	`id` uuid PRIMARY KEY NOT NULL,
	`created` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), 
);

CREATE TABLE `bets` (
	`id` bigint PRIMARY KEY NOT NULL AUTO_INCREMENT,
	`wallet` uuid NOT NULL,
	`gameId` uuid NOT NULL,
	`autoCashOut` Decimal(6, 2) NOT NULL DEFAULT 0,
	`cashedOut` Decimal(6, 2),
	`amount` Decimal(32, 18) NOT NULL,
	`amountUsd` Decimal(19, 2) NOT NULL,
	`winnings` Decimal(32, 18) NOT NULL,
	`winningsUsd` Decimal(19, 2) NOT NULL,
	FOREIGN KEY(`wallet`) REFERENCES `users`(`wallet`),
	FOREIGN KEY(`gameId`) REFERENCES `games`(`id`),
	UNIQUE(`wallet`, `gameId`)
);
