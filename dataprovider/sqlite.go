// Copyright (C) 2019-2022  Nicola Murino
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

//go:build !nosqlite
// +build !nosqlite

package dataprovider

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	// we import go-sqlite3 here to be able to disable SQLite support using a build tag
	_ "github.com/mattn/go-sqlite3"

	"github.com/drakkan/sftpgo/v2/logger"
	"github.com/drakkan/sftpgo/v2/util"
	"github.com/drakkan/sftpgo/v2/version"
	"github.com/drakkan/sftpgo/v2/vfs"
)

const (
	sqliteResetSQL = `DROP TABLE IF EXISTS "{{api_keys}}";
DROP TABLE IF EXISTS "{{folders_mapping}}";
DROP TABLE IF EXISTS "{{users_folders_mapping}}";
DROP TABLE IF EXISTS "{{users_groups_mapping}}";
DROP TABLE IF EXISTS "{{groups_folders_mapping}}";
DROP TABLE IF EXISTS "{{admins}}";
DROP TABLE IF EXISTS "{{folders}}";
DROP TABLE IF EXISTS "{{shares}}";
DROP TABLE IF EXISTS "{{users}}";
DROP TABLE IF EXISTS "{{groups}}";
DROP TABLE IF EXISTS "{{defender_events}}";
DROP TABLE IF EXISTS "{{defender_hosts}}";
DROP TABLE IF EXISTS "{{active_transfers}}";
DROP TABLE IF EXISTS "{{shared_sessions}}";
DROP TABLE IF EXISTS "{{schema_version}}";
`
	sqliteInitialSQL = `CREATE TABLE "{{schema_version}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "version" integer NOT NULL);
CREATE TABLE "{{admins}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "username" varchar(255) NOT NULL UNIQUE,
"description" varchar(512) NULL, "password" varchar(255) NOT NULL, "email" varchar(255) NULL, "status" integer NOT NULL,
"permissions" text NOT NULL, "filters" text NULL, "additional_info" text NULL, "last_login" bigint NOT NULL,
"created_at" bigint NOT NULL, "updated_at" bigint NOT NULL);
CREATE TABLE "{{defender_hosts}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "ip" varchar(50) NOT NULL UNIQUE,
"ban_time" bigint NOT NULL, "updated_at" bigint NOT NULL);
CREATE TABLE "{{defender_events}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "date_time" bigint NOT NULL,
"score" integer NOT NULL, "host_id" integer NOT NULL REFERENCES "{{defender_hosts}}" ("id") ON DELETE CASCADE
DEFERRABLE INITIALLY DEFERRED);
CREATE TABLE "{{folders}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "name" varchar(255) NOT NULL UNIQUE,
"description" varchar(512) NULL, "path" text NULL, "used_quota_size" bigint NOT NULL, "used_quota_files" integer NOT NULL,
"last_quota_update" bigint NOT NULL, "filesystem" text NULL);
CREATE TABLE "{{users}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "username" varchar(255) NOT NULL UNIQUE,
"status" integer NOT NULL, "expiration_date" bigint NOT NULL, "description" varchar(512) NULL, "password" text NULL,
"public_keys" text NULL, "home_dir" text NOT NULL, "uid" bigint NOT NULL, "gid" bigint NOT NULL,
"max_sessions" integer NOT NULL, "quota_size" bigint NOT NULL, "quota_files" integer NOT NULL, "permissions" text NOT NULL,
"used_quota_size" bigint NOT NULL, "used_quota_files" integer NOT NULL, "last_quota_update" bigint NOT NULL,
"upload_bandwidth" integer NOT NULL, "download_bandwidth" integer NOT NULL, "last_login" bigint NOT NULL, "filters" text NULL,
"filesystem" text NULL, "additional_info" text NULL, "created_at" bigint NOT NULL, "updated_at" bigint NOT NULL,
"email" varchar(255) NULL);
CREATE TABLE "{{folders_mapping}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "virtual_path" text NOT NULL,
"quota_size" bigint NOT NULL, "quota_files" integer NOT NULL, "folder_id" integer NOT NULL REFERENCES "{{folders}}" ("id")
ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED, "user_id" integer NOT NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
CONSTRAINT "{{prefix}}unique_mapping" UNIQUE ("user_id", "folder_id"));
CREATE TABLE "{{shares}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "share_id" varchar(60) NOT NULL UNIQUE,
"name" varchar(255) NOT NULL, "description" varchar(512) NULL, "scope" integer NOT NULL, "paths" text NOT NULL,
"created_at" bigint NOT NULL, "updated_at" bigint NOT NULL, "last_use_at" bigint NOT NULL, "expires_at" bigint NOT NULL,
"password" text NULL, "max_tokens" integer NOT NULL, "used_tokens" integer NOT NULL, "allow_from" text NULL,
"user_id" integer NOT NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED);
CREATE TABLE "{{api_keys}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "name" varchar(255) NOT NULL,
"key_id" varchar(50) NOT NULL UNIQUE, "api_key" varchar(255) NOT NULL UNIQUE, "scope" integer NOT NULL,
"created_at" bigint NOT NULL, "updated_at" bigint NOT NULL, "last_use_at" bigint NOT NULL, "expires_at" bigint NOT NULL,
"description" text NULL, "admin_id" integer NULL REFERENCES "{{admins}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"user_id" integer NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED);
CREATE INDEX "{{prefix}}folders_mapping_folder_id_idx" ON "{{folders_mapping}}" ("folder_id");
CREATE INDEX "{{prefix}}folders_mapping_user_id_idx" ON "{{folders_mapping}}" ("user_id");
CREATE INDEX "{{prefix}}api_keys_admin_id_idx" ON "{{api_keys}}" ("admin_id");
CREATE INDEX "{{prefix}}api_keys_user_id_idx" ON "{{api_keys}}" ("user_id");
CREATE INDEX "{{prefix}}users_updated_at_idx" ON "{{users}}" ("updated_at");
CREATE INDEX "{{prefix}}shares_user_id_idx" ON "{{shares}}" ("user_id");
CREATE INDEX "{{prefix}}defender_hosts_updated_at_idx" ON "{{defender_hosts}}" ("updated_at");
CREATE INDEX "{{prefix}}defender_hosts_ban_time_idx" ON "{{defender_hosts}}" ("ban_time");
CREATE INDEX "{{prefix}}defender_events_date_time_idx" ON "{{defender_events}}" ("date_time");
CREATE INDEX "{{prefix}}defender_events_host_id_idx" ON "{{defender_events}}" ("host_id");
INSERT INTO {{schema_version}} (version) VALUES (15);
`
	sqliteV16SQL = `ALTER TABLE "{{users}}" ADD COLUMN "download_data_transfer" integer DEFAULT 0 NOT NULL;
ALTER TABLE "{{users}}" ADD COLUMN "total_data_transfer" integer DEFAULT 0 NOT NULL;
ALTER TABLE "{{users}}" ADD COLUMN "upload_data_transfer" integer DEFAULT 0 NOT NULL;
ALTER TABLE "{{users}}" ADD COLUMN "used_download_data_transfer" integer DEFAULT 0 NOT NULL;
ALTER TABLE "{{users}}" ADD COLUMN "used_upload_data_transfer" integer DEFAULT 0 NOT NULL;
CREATE TABLE "{{active_transfers}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "connection_id" varchar(100) NOT NULL,
"transfer_id" bigint NOT NULL, "transfer_type" integer NOT NULL, "username" varchar(255) NOT NULL,
"folder_name" varchar(255) NULL, "ip" varchar(50) NOT NULL, "truncated_size" bigint NOT NULL,
"current_ul_size" bigint NOT NULL, "current_dl_size" bigint NOT NULL, "created_at" bigint NOT NULL,
"updated_at" bigint NOT NULL);
CREATE INDEX "{{prefix}}active_transfers_connection_id_idx" ON "{{active_transfers}}" ("connection_id");
CREATE INDEX "{{prefix}}active_transfers_transfer_id_idx" ON "{{active_transfers}}" ("transfer_id");
CREATE INDEX "{{prefix}}active_transfers_updated_at_idx" ON "{{active_transfers}}" ("updated_at");
`
	sqliteV16DownSQL = `ALTER TABLE "{{users}}" DROP COLUMN "used_upload_data_transfer";
ALTER TABLE "{{users}}" DROP COLUMN "used_download_data_transfer";
ALTER TABLE "{{users}}" DROP COLUMN "upload_data_transfer";
ALTER TABLE "{{users}}" DROP COLUMN "total_data_transfer";
ALTER TABLE "{{users}}" DROP COLUMN "download_data_transfer";
DROP TABLE "{{active_transfers}}";
`
	sqliteV17SQL = `CREATE TABLE "{{groups}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT, "name" varchar(255) NOT NULL UNIQUE,
"description" varchar(512) NULL, "created_at" bigint NOT NULL, "updated_at" bigint NOT NULL, "user_settings" text NULL);
CREATE TABLE "{{groups_folders_mapping}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
"folder_id" integer NOT NULL REFERENCES "{{folders}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"group_id" integer NOT NULL REFERENCES "{{groups}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"virtual_path" text NOT NULL, "quota_size" bigint NOT NULL, "quota_files" integer NOT NULL,
CONSTRAINT "{{prefix}}unique_group_folder_mapping" UNIQUE ("group_id", "folder_id"));
CREATE TABLE "{{users_groups_mapping}}" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
"user_id" integer NOT NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"group_id" integer NOT NULL REFERENCES "{{groups}}" ("id") ON DELETE NO ACTION,
"group_type" integer NOT NULL, CONSTRAINT "{{prefix}}unique_user_group_mapping" UNIQUE ("user_id", "group_id"));
CREATE TABLE "new__folders_mapping" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
"user_id" integer NOT NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"folder_id" integer NOT NULL REFERENCES "{{folders}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"virtual_path" text NOT NULL, "quota_size" bigint NOT NULL, "quota_files" integer NOT NULL,
CONSTRAINT "{{prefix}}unique_user_folder_mapping" UNIQUE ("user_id", "folder_id"));
INSERT INTO "new__folders_mapping" ("id", "virtual_path", "quota_size", "quota_files", "folder_id", "user_id") SELECT "id",
"virtual_path", "quota_size", "quota_files", "folder_id", "user_id" FROM "{{folders_mapping}}";
DROP TABLE "{{folders_mapping}}";
ALTER TABLE "new__folders_mapping" RENAME TO "{{users_folders_mapping}}";
CREATE INDEX "{{prefix}}groups_updated_at_idx" ON "{{groups}}" ("updated_at");
CREATE INDEX "{{prefix}}users_folders_mapping_folder_id_idx" ON "{{users_folders_mapping}}" ("folder_id");
CREATE INDEX "{{prefix}}users_folders_mapping_user_id_idx" ON "{{users_folders_mapping}}" ("user_id");
CREATE INDEX "{{prefix}}users_groups_mapping_group_id_idx" ON "{{users_groups_mapping}}" ("group_id");
CREATE INDEX "{{prefix}}users_groups_mapping_user_id_idx" ON "{{users_groups_mapping}}" ("user_id");
CREATE INDEX "{{prefix}}groups_folders_mapping_folder_id_idx" ON "{{groups_folders_mapping}}" ("folder_id");
CREATE INDEX "{{prefix}}groups_folders_mapping_group_id_idx" ON "{{groups_folders_mapping}}" ("group_id");
`
	sqliteV17DownSQL = `DROP TABLE "{{users_groups_mapping}}";
DROP TABLE "{{groups_folders_mapping}}";
DROP TABLE "{{groups}}";
CREATE TABLE "new__folders_mapping" ("id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
"user_id" integer NOT NULL REFERENCES "{{users}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"folder_id" integer NOT NULL REFERENCES "{{folders}}" ("id") ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
"virtual_path" text NOT NULL, "quota_size" bigint NOT NULL, "quota_files" integer NOT NULL,
CONSTRAINT "{{prefix}}unique_folder_mapping" UNIQUE ("user_id", "folder_id"));
INSERT INTO "new__folders_mapping" ("id", "virtual_path", "quota_size", "quota_files", "folder_id", "user_id") SELECT "id",
"virtual_path", "quota_size", "quota_files", "folder_id", "user_id" FROM "{{users_folders_mapping}}";
DROP TABLE "{{users_folders_mapping}}";
ALTER TABLE "new__folders_mapping" RENAME TO "{{folders_mapping}}";
CREATE INDEX "{{prefix}}folders_mapping_folder_id_idx" ON "{{folders_mapping}}" ("folder_id");
CREATE INDEX "{{prefix}}folders_mapping_user_id_idx" ON "{{folders_mapping}}" ("user_id");
`
	sqliteV19SQL = `CREATE TABLE "{{shared_sessions}}" ("key" varchar(128) NOT NULL PRIMARY KEY, "data" text NOT NULL,
"type" integer NOT NULL, "timestamp" bigint NOT NULL);
CREATE INDEX "{{prefix}}shared_sessions_type_idx" ON "{{shared_sessions}}" ("type");
CREATE INDEX "{{prefix}}shared_sessions_timestamp_idx" ON "{{shared_sessions}}" ("timestamp");
	`
	sqliteV19DownSQL = `DROP TABLE "{{shared_sessions}}";`
)

// SQLiteProvider defines the auth provider for SQLite database
type SQLiteProvider struct {
	dbHandle *sql.DB
}

func init() {
	version.AddFeature("+sqlite")
}

func initializeSQLiteProvider(basePath string) error {
	var err error
	var connectionString string

	if config.ConnectionString == "" {
		dbPath := config.Name
		if !util.IsFileInputValid(dbPath) {
			return fmt.Errorf("invalid database path: %#v", dbPath)
		}
		if !filepath.IsAbs(dbPath) {
			dbPath = filepath.Join(basePath, dbPath)
		}
		connectionString = fmt.Sprintf("file:%v?cache=shared&_foreign_keys=1", dbPath)
	} else {
		connectionString = config.ConnectionString
	}
	dbHandle, err := sql.Open("sqlite3", connectionString)
	if err == nil {
		providerLog(logger.LevelDebug, "sqlite database handle created, connection string: %#v", connectionString)
		dbHandle.SetMaxOpenConns(1)
		provider = &SQLiteProvider{dbHandle: dbHandle}
	} else {
		providerLog(logger.LevelError, "error creating sqlite database handler, connection string: %#v, error: %v",
			connectionString, err)
	}
	return err
}

func (p *SQLiteProvider) checkAvailability() error {
	return sqlCommonCheckAvailability(p.dbHandle)
}

func (p *SQLiteProvider) validateUserAndPass(username, password, ip, protocol string) (User, error) {
	return sqlCommonValidateUserAndPass(username, password, ip, protocol, p.dbHandle)
}

func (p *SQLiteProvider) validateUserAndTLSCert(username, protocol string, tlsCert *x509.Certificate) (User, error) {
	return sqlCommonValidateUserAndTLSCertificate(username, protocol, tlsCert, p.dbHandle)
}

func (p *SQLiteProvider) validateUserAndPubKey(username string, publicKey []byte, isSSHCert bool) (User, string, error) {
	return sqlCommonValidateUserAndPubKey(username, publicKey, isSSHCert, p.dbHandle)
}

func (p *SQLiteProvider) updateTransferQuota(username string, uploadSize, downloadSize int64, reset bool) error {
	return sqlCommonUpdateTransferQuota(username, uploadSize, downloadSize, reset, p.dbHandle)
}

func (p *SQLiteProvider) updateQuota(username string, filesAdd int, sizeAdd int64, reset bool) error {
	return sqlCommonUpdateQuota(username, filesAdd, sizeAdd, reset, p.dbHandle)
}

func (p *SQLiteProvider) getUsedQuota(username string) (int, int64, int64, int64, error) {
	return sqlCommonGetUsedQuota(username, p.dbHandle)
}

func (p *SQLiteProvider) setUpdatedAt(username string) {
	sqlCommonSetUpdatedAt(username, p.dbHandle)
}

func (p *SQLiteProvider) updateLastLogin(username string) error {
	return sqlCommonUpdateLastLogin(username, p.dbHandle)
}

func (p *SQLiteProvider) updateAdminLastLogin(username string) error {
	return sqlCommonUpdateAdminLastLogin(username, p.dbHandle)
}

func (p *SQLiteProvider) userExists(username string) (User, error) {
	return sqlCommonGetUserByUsername(username, p.dbHandle)
}

func (p *SQLiteProvider) addUser(user *User) error {
	return sqlCommonAddUser(user, p.dbHandle)
}

func (p *SQLiteProvider) updateUser(user *User) error {
	return sqlCommonUpdateUser(user, p.dbHandle)
}

func (p *SQLiteProvider) deleteUser(user User) error {
	return sqlCommonDeleteUser(user, p.dbHandle)
}

func (p *SQLiteProvider) updateUserPassword(username, password string) error {
	return sqlCommonUpdateUserPassword(username, password, p.dbHandle)
}

func (p *SQLiteProvider) dumpUsers() ([]User, error) {
	return sqlCommonDumpUsers(p.dbHandle)
}

func (p *SQLiteProvider) getRecentlyUpdatedUsers(after int64) ([]User, error) {
	return sqlCommonGetRecentlyUpdatedUsers(after, p.dbHandle)
}

func (p *SQLiteProvider) getUsers(limit int, offset int, order string) ([]User, error) {
	return sqlCommonGetUsers(limit, offset, order, p.dbHandle)
}

func (p *SQLiteProvider) getUsersForQuotaCheck(toFetch map[string]bool) ([]User, error) {
	return sqlCommonGetUsersForQuotaCheck(toFetch, p.dbHandle)
}

func (p *SQLiteProvider) dumpFolders() ([]vfs.BaseVirtualFolder, error) {
	return sqlCommonDumpFolders(p.dbHandle)
}

func (p *SQLiteProvider) getFolders(limit, offset int, order string, minimal bool) ([]vfs.BaseVirtualFolder, error) {
	return sqlCommonGetFolders(limit, offset, order, minimal, p.dbHandle)
}

func (p *SQLiteProvider) getFolderByName(name string) (vfs.BaseVirtualFolder, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultSQLQueryTimeout)
	defer cancel()
	return sqlCommonGetFolderByName(ctx, name, p.dbHandle)
}

func (p *SQLiteProvider) addFolder(folder *vfs.BaseVirtualFolder) error {
	return sqlCommonAddFolder(folder, p.dbHandle)
}

func (p *SQLiteProvider) updateFolder(folder *vfs.BaseVirtualFolder) error {
	return sqlCommonUpdateFolder(folder, p.dbHandle)
}

func (p *SQLiteProvider) deleteFolder(folder vfs.BaseVirtualFolder) error {
	return sqlCommonDeleteFolder(folder, p.dbHandle)
}

func (p *SQLiteProvider) updateFolderQuota(name string, filesAdd int, sizeAdd int64, reset bool) error {
	return sqlCommonUpdateFolderQuota(name, filesAdd, sizeAdd, reset, p.dbHandle)
}

func (p *SQLiteProvider) getUsedFolderQuota(name string) (int, int64, error) {
	return sqlCommonGetFolderUsedQuota(name, p.dbHandle)
}

func (p *SQLiteProvider) getGroups(limit, offset int, order string, minimal bool) ([]Group, error) {
	return sqlCommonGetGroups(limit, offset, order, minimal, p.dbHandle)
}

func (p *SQLiteProvider) getGroupsWithNames(names []string) ([]Group, error) {
	return sqlCommonGetGroupsWithNames(names, p.dbHandle)
}

func (p *SQLiteProvider) getUsersInGroups(names []string) ([]string, error) {
	return sqlCommonGetUsersInGroups(names, p.dbHandle)
}

func (p *SQLiteProvider) groupExists(name string) (Group, error) {
	return sqlCommonGetGroupByName(name, p.dbHandle)
}

func (p *SQLiteProvider) addGroup(group *Group) error {
	return sqlCommonAddGroup(group, p.dbHandle)
}

func (p *SQLiteProvider) updateGroup(group *Group) error {
	return sqlCommonUpdateGroup(group, p.dbHandle)
}

func (p *SQLiteProvider) deleteGroup(group Group) error {
	return sqlCommonDeleteGroup(group, p.dbHandle)
}

func (p *SQLiteProvider) dumpGroups() ([]Group, error) {
	return sqlCommonDumpGroups(p.dbHandle)
}

func (p *SQLiteProvider) adminExists(username string) (Admin, error) {
	return sqlCommonGetAdminByUsername(username, p.dbHandle)
}

func (p *SQLiteProvider) addAdmin(admin *Admin) error {
	return sqlCommonAddAdmin(admin, p.dbHandle)
}

func (p *SQLiteProvider) updateAdmin(admin *Admin) error {
	return sqlCommonUpdateAdmin(admin, p.dbHandle)
}

func (p *SQLiteProvider) deleteAdmin(admin Admin) error {
	return sqlCommonDeleteAdmin(admin, p.dbHandle)
}

func (p *SQLiteProvider) getAdmins(limit int, offset int, order string) ([]Admin, error) {
	return sqlCommonGetAdmins(limit, offset, order, p.dbHandle)
}

func (p *SQLiteProvider) dumpAdmins() ([]Admin, error) {
	return sqlCommonDumpAdmins(p.dbHandle)
}

func (p *SQLiteProvider) validateAdminAndPass(username, password, ip string) (Admin, error) {
	return sqlCommonValidateAdminAndPass(username, password, ip, p.dbHandle)
}

func (p *SQLiteProvider) apiKeyExists(keyID string) (APIKey, error) {
	return sqlCommonGetAPIKeyByID(keyID, p.dbHandle)
}

func (p *SQLiteProvider) addAPIKey(apiKey *APIKey) error {
	return sqlCommonAddAPIKey(apiKey, p.dbHandle)
}

func (p *SQLiteProvider) updateAPIKey(apiKey *APIKey) error {
	return sqlCommonUpdateAPIKey(apiKey, p.dbHandle)
}

func (p *SQLiteProvider) deleteAPIKey(apiKey APIKey) error {
	return sqlCommonDeleteAPIKey(apiKey, p.dbHandle)
}

func (p *SQLiteProvider) getAPIKeys(limit int, offset int, order string) ([]APIKey, error) {
	return sqlCommonGetAPIKeys(limit, offset, order, p.dbHandle)
}

func (p *SQLiteProvider) dumpAPIKeys() ([]APIKey, error) {
	return sqlCommonDumpAPIKeys(p.dbHandle)
}

func (p *SQLiteProvider) updateAPIKeyLastUse(keyID string) error {
	return sqlCommonUpdateAPIKeyLastUse(keyID, p.dbHandle)
}

func (p *SQLiteProvider) shareExists(shareID, username string) (Share, error) {
	return sqlCommonGetShareByID(shareID, username, p.dbHandle)
}

func (p *SQLiteProvider) addShare(share *Share) error {
	return sqlCommonAddShare(share, p.dbHandle)
}

func (p *SQLiteProvider) updateShare(share *Share) error {
	return sqlCommonUpdateShare(share, p.dbHandle)
}

func (p *SQLiteProvider) deleteShare(share Share) error {
	return sqlCommonDeleteShare(share, p.dbHandle)
}

func (p *SQLiteProvider) getShares(limit int, offset int, order, username string) ([]Share, error) {
	return sqlCommonGetShares(limit, offset, order, username, p.dbHandle)
}

func (p *SQLiteProvider) dumpShares() ([]Share, error) {
	return sqlCommonDumpShares(p.dbHandle)
}

func (p *SQLiteProvider) updateShareLastUse(shareID string, numTokens int) error {
	return sqlCommonUpdateShareLastUse(shareID, numTokens, p.dbHandle)
}

func (p *SQLiteProvider) getDefenderHosts(from int64, limit int) ([]DefenderEntry, error) {
	return sqlCommonGetDefenderHosts(from, limit, p.dbHandle)
}

func (p *SQLiteProvider) getDefenderHostByIP(ip string, from int64) (DefenderEntry, error) {
	return sqlCommonGetDefenderHostByIP(ip, from, p.dbHandle)
}

func (p *SQLiteProvider) isDefenderHostBanned(ip string) (DefenderEntry, error) {
	return sqlCommonIsDefenderHostBanned(ip, p.dbHandle)
}

func (p *SQLiteProvider) updateDefenderBanTime(ip string, minutes int) error {
	return sqlCommonDefenderIncrementBanTime(ip, minutes, p.dbHandle)
}

func (p *SQLiteProvider) deleteDefenderHost(ip string) error {
	return sqlCommonDeleteDefenderHost(ip, p.dbHandle)
}

func (p *SQLiteProvider) addDefenderEvent(ip string, score int) error {
	return sqlCommonAddDefenderHostAndEvent(ip, score, p.dbHandle)
}

func (p *SQLiteProvider) setDefenderBanTime(ip string, banTime int64) error {
	return sqlCommonSetDefenderBanTime(ip, banTime, p.dbHandle)
}

func (p *SQLiteProvider) cleanupDefender(from int64) error {
	return sqlCommonDefenderCleanup(from, p.dbHandle)
}

func (p *SQLiteProvider) addActiveTransfer(transfer ActiveTransfer) error {
	return sqlCommonAddActiveTransfer(transfer, p.dbHandle)
}

func (p *SQLiteProvider) updateActiveTransferSizes(ulSize, dlSize, transferID int64, connectionID string) error {
	return sqlCommonUpdateActiveTransferSizes(ulSize, dlSize, transferID, connectionID, p.dbHandle)
}

func (p *SQLiteProvider) removeActiveTransfer(transferID int64, connectionID string) error {
	return sqlCommonRemoveActiveTransfer(transferID, connectionID, p.dbHandle)
}

func (p *SQLiteProvider) cleanupActiveTransfers(before time.Time) error {
	return sqlCommonCleanupActiveTransfers(before, p.dbHandle)
}

func (p *SQLiteProvider) getActiveTransfers(from time.Time) ([]ActiveTransfer, error) {
	return sqlCommonGetActiveTransfers(from, p.dbHandle)
}

func (p *SQLiteProvider) addSharedSession(session Session) error {
	return sqlCommonAddSession(session, p.dbHandle)
}

func (p *SQLiteProvider) deleteSharedSession(key string) error {
	return sqlCommonDeleteSession(key, p.dbHandle)
}

func (p *SQLiteProvider) getSharedSession(key string) (Session, error) {
	return sqlCommonGetSession(key, p.dbHandle)
}

func (p *SQLiteProvider) cleanupSharedSessions(sessionType SessionType, before int64) error {
	return sqlCommonCleanupSessions(sessionType, before, p.dbHandle)
}

func (p *SQLiteProvider) close() error {
	return p.dbHandle.Close()
}

func (p *SQLiteProvider) reloadConfig() error {
	return nil
}

// initializeDatabase creates the initial database structure
func (p *SQLiteProvider) initializeDatabase() error {
	dbVersion, err := sqlCommonGetDatabaseVersion(p.dbHandle, false)
	if err == nil && dbVersion.Version > 0 {
		return ErrNoInitRequired
	}
	if errors.Is(err, sql.ErrNoRows) {
		return errSchemaVersionEmpty
	}
	logger.InfoToConsole("creating initial database schema, version 15")
	providerLog(logger.LevelInfo, "creating initial database schema, version 15")
	initialSQL := strings.ReplaceAll(sqliteInitialSQL, "{{schema_version}}", sqlTableSchemaVersion)
	initialSQL = strings.ReplaceAll(initialSQL, "{{admins}}", sqlTableAdmins)
	initialSQL = strings.ReplaceAll(initialSQL, "{{folders}}", sqlTableFolders)
	initialSQL = strings.ReplaceAll(initialSQL, "{{users}}", sqlTableUsers)
	initialSQL = strings.ReplaceAll(initialSQL, "{{folders_mapping}}", sqlTableFoldersMapping)
	initialSQL = strings.ReplaceAll(initialSQL, "{{api_keys}}", sqlTableAPIKeys)
	initialSQL = strings.ReplaceAll(initialSQL, "{{shares}}", sqlTableShares)
	initialSQL = strings.ReplaceAll(initialSQL, "{{defender_events}}", sqlTableDefenderEvents)
	initialSQL = strings.ReplaceAll(initialSQL, "{{defender_hosts}}", sqlTableDefenderHosts)
	initialSQL = strings.ReplaceAll(initialSQL, "{{prefix}}", config.SQLTablesPrefix)

	return sqlCommonExecSQLAndUpdateDBVersion(p.dbHandle, []string{initialSQL}, 15, true)
}

func (p *SQLiteProvider) migrateDatabase() error { //nolint:dupl
	dbVersion, err := sqlCommonGetDatabaseVersion(p.dbHandle, true)
	if err != nil {
		return err
	}

	switch version := dbVersion.Version; {
	case version == sqlDatabaseVersion:
		providerLog(logger.LevelDebug, "sql database is up to date, current version: %v", version)
		return ErrNoInitRequired
	case version < 15:
		err = fmt.Errorf("database version %v is too old, please see the upgrading docs", version)
		providerLog(logger.LevelError, "%v", err)
		logger.ErrorToConsole("%v", err)
		return err
	case version == 15:
		return updateSQLiteDatabaseFromV15(p.dbHandle)
	case version == 16:
		return updateSQLiteDatabaseFromV16(p.dbHandle)
	case version == 17:
		return updateSQLiteDatabaseFromV17(p.dbHandle)
	case version == 18:
		return updateSQLiteDatabaseFromV18(p.dbHandle)
	default:
		if version > sqlDatabaseVersion {
			providerLog(logger.LevelError, "database version %v is newer than the supported one: %v", version,
				sqlDatabaseVersion)
			logger.WarnToConsole("database version %v is newer than the supported one: %v", version,
				sqlDatabaseVersion)
			return nil
		}
		return fmt.Errorf("database version not handled: %v", version)
	}
}

func (p *SQLiteProvider) revertDatabase(targetVersion int) error {
	dbVersion, err := sqlCommonGetDatabaseVersion(p.dbHandle, true)
	if err != nil {
		return err
	}
	if dbVersion.Version == targetVersion {
		return errors.New("current version match target version, nothing to do")
	}

	switch dbVersion.Version {
	case 16:
		return downgradeSQLiteDatabaseFromV16(p.dbHandle)
	case 17:
		return downgradeSQLiteDatabaseFromV17(p.dbHandle)
	case 18:
		return downgradeSQLiteDatabaseFromV18(p.dbHandle)
	case 19:
		return downgradeSQLiteDatabaseFromV19(p.dbHandle)
	default:
		return fmt.Errorf("database version not handled: %v", dbVersion.Version)
	}
}

func (p *SQLiteProvider) resetDatabase() error {
	sql := sqlReplaceAll(sqliteResetSQL)
	return sqlCommonExecSQLAndUpdateDBVersion(p.dbHandle, []string{sql}, 0, false)
}

func updateSQLiteDatabaseFromV15(dbHandle *sql.DB) error {
	if err := updateSQLiteDatabaseFrom15To16(dbHandle); err != nil {
		return err
	}
	return updateSQLiteDatabaseFromV16(dbHandle)
}

func updateSQLiteDatabaseFromV16(dbHandle *sql.DB) error {
	if err := updateSQLiteDatabaseFrom16To17(dbHandle); err != nil {
		return err
	}
	return updateSQLiteDatabaseFromV17(dbHandle)
}

func updateSQLiteDatabaseFromV17(dbHandle *sql.DB) error {
	if err := updateSQLiteDatabaseFrom17To18(dbHandle); err != nil {
		return err
	}
	return updateSQLiteDatabaseFromV18(dbHandle)
}

func updateSQLiteDatabaseFromV18(dbHandle *sql.DB) error {
	return updateSQLiteDatabaseFrom18To19(dbHandle)
}

func downgradeSQLiteDatabaseFromV16(dbHandle *sql.DB) error {
	return downgradeSQLiteDatabaseFrom16To15(dbHandle)
}

func downgradeSQLiteDatabaseFromV17(dbHandle *sql.DB) error {
	if err := downgradeSQLiteDatabaseFrom17To16(dbHandle); err != nil {
		return err
	}
	return downgradeSQLiteDatabaseFromV16(dbHandle)
}

func downgradeSQLiteDatabaseFromV18(dbHandle *sql.DB) error {
	if err := downgradeSQLiteDatabaseFrom18To17(dbHandle); err != nil {
		return err
	}
	return downgradeSQLiteDatabaseFromV17(dbHandle)
}

func downgradeSQLiteDatabaseFromV19(dbHandle *sql.DB) error {
	if err := downgradeSQLiteDatabaseFrom19To18(dbHandle); err != nil {
		return err
	}
	return downgradeSQLiteDatabaseFromV18(dbHandle)
}

func updateSQLiteDatabaseFrom15To16(dbHandle *sql.DB) error {
	logger.InfoToConsole("updating database version: 15 -> 16")
	providerLog(logger.LevelInfo, "updating database version: 15 -> 16")
	sql := strings.ReplaceAll(sqliteV16SQL, "{{users}}", sqlTableUsers)
	sql = strings.ReplaceAll(sql, "{{active_transfers}}", sqlTableActiveTransfers)
	sql = strings.ReplaceAll(sql, "{{prefix}}", config.SQLTablesPrefix)
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 16, true)
}

func updateSQLiteDatabaseFrom16To17(dbHandle *sql.DB) error {
	logger.InfoToConsole("updating database version: 16 -> 17")
	providerLog(logger.LevelInfo, "updating database version: 16 -> 17")
	if err := setPragmaFK(dbHandle, "OFF"); err != nil {
		return err
	}
	sql := strings.ReplaceAll(sqliteV17SQL, "{{users}}", sqlTableUsers)
	sql = strings.ReplaceAll(sql, "{{groups}}", sqlTableGroups)
	sql = strings.ReplaceAll(sql, "{{users}}", sqlTableUsers)
	sql = strings.ReplaceAll(sql, "{{folders}}", sqlTableFolders)
	sql = strings.ReplaceAll(sql, "{{folders_mapping}}", sqlTableFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{users_folders_mapping}}", sqlTableUsersFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{users_groups_mapping}}", sqlTableUsersGroupsMapping)
	sql = strings.ReplaceAll(sql, "{{groups_folders_mapping}}", sqlTableGroupsFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{prefix}}", config.SQLTablesPrefix)
	if err := sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 17, true); err != nil {
		return err
	}
	return setPragmaFK(dbHandle, "ON")
}

func updateSQLiteDatabaseFrom17To18(dbHandle *sql.DB) error {
	logger.InfoToConsole("updating database version: 17 -> 18")
	providerLog(logger.LevelInfo, "updating database version: 17 -> 18")
	if err := importGCSCredentials(); err != nil {
		return err
	}
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, nil, 18, true)
}

func updateSQLiteDatabaseFrom18To19(dbHandle *sql.DB) error {
	logger.InfoToConsole("updating database version: 18 -> 19")
	providerLog(logger.LevelInfo, "updating database version: 18 -> 19")
	sql := strings.ReplaceAll(sqliteV19SQL, "{{shared_sessions}}", sqlTableSharedSessions)
	sql = strings.ReplaceAll(sql, "{{prefix}}", config.SQLTablesPrefix)
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 19, true)
}

func downgradeSQLiteDatabaseFrom16To15(dbHandle *sql.DB) error {
	logger.InfoToConsole("downgrading database version: 16 -> 15")
	providerLog(logger.LevelInfo, "downgrading database version: 16 -> 15")
	sql := strings.ReplaceAll(sqliteV16DownSQL, "{{users}}", sqlTableUsers)
	sql = strings.ReplaceAll(sql, "{{active_transfers}}", sqlTableActiveTransfers)
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 15, false)
}

func downgradeSQLiteDatabaseFrom17To16(dbHandle *sql.DB) error {
	logger.InfoToConsole("downgrading database version: 17 -> 16")
	providerLog(logger.LevelInfo, "downgrading database version: 17 -> 16")
	if err := setPragmaFK(dbHandle, "OFF"); err != nil {
		return err
	}
	sql := strings.ReplaceAll(sqliteV17DownSQL, "{{groups}}", sqlTableGroups)
	sql = strings.ReplaceAll(sql, "{{users}}", sqlTableUsers)
	sql = strings.ReplaceAll(sql, "{{folders}}", sqlTableFolders)
	sql = strings.ReplaceAll(sql, "{{folders_mapping}}", sqlTableFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{users_folders_mapping}}", sqlTableUsersFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{users_groups_mapping}}", sqlTableUsersGroupsMapping)
	sql = strings.ReplaceAll(sql, "{{groups_folders_mapping}}", sqlTableGroupsFoldersMapping)
	sql = strings.ReplaceAll(sql, "{{prefix}}", config.SQLTablesPrefix)
	if err := sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 16, false); err != nil {
		return err
	}
	return setPragmaFK(dbHandle, "ON")
}

func downgradeSQLiteDatabaseFrom18To17(dbHandle *sql.DB) error {
	logger.InfoToConsole("downgrading database version: 18 -> 17")
	providerLog(logger.LevelInfo, "downgrading database version: 18 -> 17")
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, nil, 17, false)
}

func downgradeSQLiteDatabaseFrom19To18(dbHandle *sql.DB) error {
	logger.InfoToConsole("downgrading database version: 19 -> 18")
	providerLog(logger.LevelInfo, "downgrading database version: 19 -> 18")
	sql := strings.ReplaceAll(sqliteV19DownSQL, "{{shared_sessions}}", sqlTableSharedSessions)
	return sqlCommonExecSQLAndUpdateDBVersion(dbHandle, []string{sql}, 18, false)
}

func setPragmaFK(dbHandle *sql.DB, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), longSQLQueryTimeout)
	defer cancel()

	sql := fmt.Sprintf("PRAGMA foreign_keys=%v;", value)

	_, err := dbHandle.ExecContext(ctx, sql)
	return err
}
