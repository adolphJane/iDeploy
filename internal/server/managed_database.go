package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ideploy/internal/databasebrowser"
)

var managedDatabaseID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type databaseEngine struct {
	image, dataDir      string
	port                int
	variablePrefix      string
	connect, dump, load string
}

func managedDatabaseEngine(instance databasebrowser.Instance) (databaseEngine, error) {
	if !managedDatabaseID.MatchString(instance.ID) {
		return databaseEngine{}, errors.New("数据库实例 ID 无效")
	}
	config, err := instance.InstanceConfig.Validate()
	if err != nil {
		return databaseEngine{}, err
	}
	instance.InstanceConfig = config
	switch instance.Driver {
	case databasebrowser.DriverPostgres:
		const auth = `export PGPASSWORD="$(cat /run/ideploy-db-password)"; `
		const address = " -h 127.0.0.1 -U postgres -d "
		appAuth := `export PGPASSWORD="$(cat /run/ideploy-db-app-password)"; psql -w -h 127.0.0.1 -U ` + instance.AppUsername + " -d " + instance.DatabaseName
		return databaseEngine{
			image: "docker.io/library/postgres:" + instance.EngineVersion, dataDir: "/var/lib/postgresql/data",
			port: 5432, variablePrefix: "POSTGRES_",
			connect: appAuth + " -Atc 'SELECT 1'",
			dump:    auth + "pg_dump -w" + address + instance.DatabaseName + " --no-owner --no-privileges",
			load:    appAuth + " --single-transaction -v ON_ERROR_STOP=1",
		}, nil
	case databasebrowser.DriverMySQL:
		const auth = `export MYSQL_PWD="$(cat /run/ideploy-db-password)"; `
		const address = " -uroot "
		return databaseEngine{
			image: "docker.io/library/mysql:" + instance.EngineVersion, dataDir: "/var/lib/mysql", port: 3306,
			variablePrefix: "MYSQL_ROOT_",
			connect:        `export MYSQL_PWD="$(cat /run/ideploy-db-app-password)"; mysql --protocol=TCP -h 127.0.0.1 -u` + instance.AppUsername + " " + instance.DatabaseName + " -Nse 'SELECT 1'",
			dump:           auth + "mysqldump" + address + " --single-transaction --routines --events --no-tablespaces --set-gtid-purged=OFF " + instance.DatabaseName,
			load:           auth + "mysql" + address + instance.DatabaseName,
		}, nil
	default:
		return databaseEngine{}, errors.New("数据库引擎无效")
	}
}

func managedContainer(instance databasebrowser.Instance) string { return "ideploy-db-" + instance.ID }

func managedDatabaseDir(id string) string { return ".ideploy/databases/" + id }

func managedDatabaseOwner(name, id string) string {
	return `[ "$(docker inspect --format '{{ index .Config.Labels "com.ideploy.database-instance" }}' ` + shellQuote(name) + ")" + `" = ` + shellQuote(id) + " ]"
}

func (r *Remote) DeployManagedDatabase(ctx context.Context, host Connection, instance databasebrowser.Instance, password, appPassword string) error {
	engine, err := managedDatabaseEngine(instance)
	if err != nil {
		return err
	}
	config, err := instance.InstanceConfig.Validate()
	if err != nil {
		return err
	}
	instance.InstanceConfig = config
	name := managedContainer(instance)
	dir := managedDatabaseDir(instance.ID)
	volume := name + "-data"
	dbVariable := "POSTGRES_DB"
	if instance.Driver == databasebrowser.DriverMySQL {
		dbVariable = "MYSQL_DATABASE"
	}
	args := []string{
		"docker", "run", "-d", "--name", shellQuote(name), "--restart", "unless-stopped",
		"--label", shellQuote("com.ideploy.database-instance=" + instance.ID),
		"--label", shellQuote("com.ideploy.environment=" + instance.Environment),
		"--publish", shellQuote(instance.ListenIP + ":" + strconv.Itoa(instance.Port) + ":" + strconv.Itoa(engine.port)),
		"--mount", shellQuote("type=volume,src=" + volume + ",dst=" + engine.dataDir),
		"--mount", `"type=bind,src=$HOME/` + dir + `/password,dst=/run/ideploy-db-password,readonly"`,
		"--mount", `"type=bind,src=$HOME/` + dir + `/app-password,dst=/run/ideploy-db-app-password,readonly"`,
		"--env", shellQuote(engine.variablePrefix + "PASSWORD_FILE=/run/ideploy-db-password"),
		"--env", shellQuote(dbVariable + "=" + instance.DatabaseName),
	}
	extraSetup := ""
	if instance.Driver == databasebrowser.DriverMySQL {
		args = append(args, "--env", "MYSQL_ROOT_HOST=localhost", "--env", "MYSQL_USER="+instance.AppUsername,
			"--env", "MYSQL_PASSWORD_FILE=/run/ideploy-db-app-password")
	} else {
		args = append(args, "--env", "IDEPLOY_DB_APP_USER="+instance.AppUsername,
			"--mount", `"type=bind,src=$HOME/`+dir+`/init-app.sh,dst=/docker-entrypoint-initdb.d/10-ideploy-app.sh,readonly"`)
		extraSetup = `
cat > "$HOME/` + dir + `/init-app.sh" <<'IDEPLOY_PG'
app_password="$(cat /run/ideploy-db-app-password)"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    -v app_user="$IDEPLOY_DB_APP_USER" -v database_name="$POSTGRES_DB" -v app_password="$app_password" <<'IDEPLOY_SQL'
CREATE ROLE :"app_user" LOGIN PASSWORD :'app_password';
ALTER DATABASE :"database_name" OWNER TO :"app_user";
IDEPLOY_SQL
unset app_password
IDEPLOY_PG
chmod 644 "$HOME/` + dir + `/init-app.sh"
chmod 444 "$HOME/` + dir + `/app-password"`
	}
	args = append(args, `"$image_id"`)
	command := `set -eu
if docker container inspect ` + shellQuote(name) + ` >/dev/null 2>&1; then
	` + managedDatabaseOwner(name, instance.ID) + ` || exit 31
	docker start ` + shellQuote(name) + ` >/dev/null
else
	umask 077
	mkdir -p "$HOME/` + dir + `"
	IFS= read -r admin_secret
	IFS= read -r app_secret
	save_secret() {
		file="$1"
		tmp="$(mktemp "$file.XXXXXX")"
		trap 'rm -f "$tmp"' EXIT HUP INT TERM
		printf '%s' "$2" > "$tmp"
		if test -f "$file"; then
			cmp -s "$file" "$tmp" || exit 32
			rm -f "$tmp"
		else
			mv "$tmp" "$file"
		fi
		trap - EXIT HUP INT TERM
	}
	save_secret "$HOME/` + dir + `/password" "$admin_secret"
	save_secret "$HOME/` + dir + `/app-password" "$app_secret"
	unset admin_secret app_secret` + extraSetup + `
	if docker volume inspect ` + shellQuote(volume) + ` >/dev/null 2>&1; then
		[ "$(docker volume inspect --format '{{index .Labels "com.ideploy.database-instance"}}' ` + shellQuote(volume) + `)" = ` + shellQuote(instance.ID) + ` ] || exit 33
	else
		docker volume create --label ` + shellQuote("com.ideploy.database-instance="+instance.ID) + " " + shellQuote(volume) + ` >/dev/null
	fi
	docker pull ` + shellQuote(engine.image) + ` >/dev/null
	image_id="$(docker image inspect --format '{{.Id}}' ` + shellQuote(engine.image) + `)"
	` + strings.Join(args, " ") + ` >/dev/null
fi
i=0
until docker exec ` + shellQuote(name) + ` sh -ec ` + shellQuote(engine.connect) + ` >/dev/null 2>&1; do
	i=$((i+1))
	test "$i" -lt 60 || exit 34
	sleep 2
done`
	if _, err := r.runTimeoutWithInput(ctx, host, command, 18*time.Minute, password+"\n"+appPassword+"\n"); err != nil {
		return errors.New("数据库启动或认证检查失败，请在目标服务器核对镜像、监听端口和容器状态")
	}
	return nil
}

func (r *Remote) BackupManagedDatabase(ctx context.Context, host Connection, instance databasebrowser.Instance, id string) error {
	engine, err := managedDatabaseEngine(instance)
	if err != nil || !managedDatabaseID.MatchString(id) {
		return errors.New("数据库备份标识无效")
	}
	name := managedContainer(instance)
	directory := managedDatabaseDir(instance.ID) + "/backups"
	command := `set -eu
` + managedDatabaseOwner(name, instance.ID) + ` || exit 31
umask 077
mkdir -p "$HOME/` + directory + `"
cd "$HOME/` + directory + `"
tmp="$(mktemp ` + id + `.XXXXXX)"
trap 'rm -f "$tmp"' EXIT HUP INT TERM
docker exec ` + shellQuote(name) + " sh -ec " + shellQuote(engine.dump) + ` > "$tmp"
test -s "$tmp"
mv "$tmp" ` + id + `.sql`
	if _, err := r.runTimeout(ctx, host, command, 60*time.Minute); err != nil {
		return errors.New("生成数据库备份失败，请检查实例状态和服务器磁盘空间")
	}
	return nil
}

func (r *Remote) RestoreManagedDatabase(ctx context.Context, host Connection, sourceID, backupID string, target databasebrowser.Instance) error {
	engine, err := managedDatabaseEngine(target)
	if err != nil || !managedDatabaseID.MatchString(sourceID) || !managedDatabaseID.MatchString(backupID) {
		return errors.New("恢复任务标识无效")
	}
	name := managedContainer(target)
	backup := managedDatabaseDir(sourceID) + "/backups/" + backupID + ".sql"
	command := fmt.Sprintf(`set -eu
%s || exit 31
test -s "$HOME/%s"
docker exec -i %s sh -ec %s < "$HOME/%s" >/dev/null 2>&1
docker exec %s sh -ec %s >/dev/null 2>&1`,
		managedDatabaseOwner(name, target.ID), backup, shellQuote(name), shellQuote(engine.load), backup,
		shellQuote(name), shellQuote(engine.connect))
	if _, err := r.runTimeout(ctx, host, command, 60*time.Minute); err != nil {
		return errors.New("恢复数据库失败，该实例可能含有部分恢复的数据")
	}
	return nil
}

func (r *Remote) StopManagedDatabaseAfterFailedRestore(host Connection, instance databasebrowser.Instance) {
	if !managedDatabaseID.MatchString(instance.ID) {
		return
	}
	name := managedContainer(instance)
	command := managedDatabaseOwner(name, instance.ID) + " && docker update --restart=no " + shellQuote(name) + " >/dev/null && docker stop " + shellQuote(name) + " >/dev/null"
	_, _ = r.runTimeout(context.Background(), host, command, 25*time.Second)
}
