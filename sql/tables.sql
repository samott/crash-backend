DROP TABLE IF EXISTS `ledger`;
DROP TABLE IF EXISTS `users`;
DROP TABLE IF EXISTS `bets`;
DROP TABLE IF EXISTS `games`;
DROP TABLE IF EXISTS `balances`;

CREATE TABLE `games` (
	`id` uuid PRIMARY KEY NOT NULL,
	`created` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
);

CREATE TABLE `bets` (
	`id` uuid PRIMARY KEY NOT NULL,
	`wallet` char(42) NOT NULL,
	`gameId` uuid NOT NULL,
	`autoCashOut` Decimal(6, 2) NOT NULL DEFAULT 0,
	`cashedOut` Decimal(6, 2),
	`amount` Decimal(32, 18) unsigned NOT NULL,
	`amountUsd` Decimal(19, 2) unsigned NOT NULL,
	`winnings` Decimal(32, 18) unsigned NOT NULL,
	`winningsUsd` Decimal(19, 2) unsigned NOT NULL,
	`created` datetime(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	FOREIGN KEY(`gameId`) REFERENCES `games`(`id`),
	UNIQUE (`wallet`, `gameId`)
);

CREATE TABLE `balances` (
	`wallet` char(42) NOT NULL,
	`currency` varchar(32) NOT NULL,
	`gained` Decimal(32, 18) unsigned NOT NULL DEFAULT 0,
	`spent` Decimal(32, 18) unsigned NOT NULL DEFAULT 0,
	`balance` Decimal(32, 18) unsigned NOT NULL DEFAULT 0,
	UNIQUE (`wallet`, `currency`)
);

CREATE TABLE `ledger` (
	`id` bigint PRIMARY KEY NOT NULL AUTO_INCREMENT,
	`wallet` char(42) NOT NULL,
	`currency` varchar(32) NOT NULL,
	`change` Decimal(32, 18) NOT NULL,
	`gameId` uuid NOT NULL,
	FOREIGN KEY(`betId`) REFERENCES `bets`(`id`)
);
