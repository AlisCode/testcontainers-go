package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"
	"github.com/mdelapenya/tlscert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	dbname   = "test-db"
	user     = "postgres"
	password = "password"
)

func createSSLCerts(t *testing.T) (*tlscert.Certificate, *tlscert.Certificate, error) {
	t.Helper()
	tmpDir := t.TempDir()
	certsDir := tmpDir + "/certs"

	require.NoError(t, os.MkdirAll(certsDir, 0o755))

	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(tmpDir))
	})

	caCert := tlscert.SelfSignedFromRequest(tlscert.Request{
		Host:      "localhost",
		Name:      "ca-cert",
		ParentDir: certsDir,
	})

	if caCert == nil {
		return caCert, nil, errors.New("unable to create CA Authority")
	}

	cert := tlscert.SelfSignedFromRequest(tlscert.Request{
		Host:      "localhost",
		Name:      "client-cert",
		Parent:    caCert,
		ParentDir: certsDir,
	})
	if cert == nil {
		return caCert, cert, errors.New("unable to create Server Certificates")
	}

	return caCert, cert, nil
}

func TestPostgres(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		image string
	}{
		{
			name:  "Postgres",
			image: "postgres:15.2-alpine",
		},
		{
			name: "Timescale",
			// timescale {
			image: "timescale/timescaledb:2.1.0-pg11",
			// }
		},
		{
			name: "Postgis",
			// postgis {
			image: "postgis/postgis:12-3.0",
			// }
		},
		{
			name: "Pgvector",
			// pgvector {
			image: "pgvector/pgvector:pg16",
			// }
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctr, err := postgres.Run(ctx,
				tt.image,
				postgres.WithDatabase(dbname),
				postgres.WithUsername(user),
				postgres.WithPassword(password),
				postgres.BasicWaitStrategies(),
			)
			testcontainers.CleanupContainer(t, ctr)
			require.NoError(t, err)

			// connectionString {
			// explicitly set sslmode=disable because the container is not configured to use TLS
			connStr, err := ctr.ConnectionString(ctx, "sslmode=disable", "application_name=test")
			// }
			require.NoError(t, err)

			mustConnStr := ctr.MustConnectionString(ctx, "sslmode=disable", "application_name=test")
			require.Equalf(t, mustConnStr, connStr, "ConnectionString was not equal to MustConnectionString")

			// Ensure connection string is using generic format
			id, err := ctr.MappedPort(ctx, "5432/tcp")
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&application_name=test", user, password, "localhost", id.Port(), dbname), connStr)

			// perform assertions
			db, err := sql.Open("postgres", connStr)
			require.NoError(t, err)
			require.NotNil(t, db)
			defer db.Close()

			result, err := db.Exec("CREATE TABLE IF NOT EXISTS test (id int, name varchar(255));")
			require.NoError(t, err)
			require.NotNil(t, result)

			result, err = db.Exec("INSERT INTO test (id, name) VALUES (1, 'test');")
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestContainerWithWaitForSQL(t *testing.T) {
	ctx := context.Background()

	port := "5432/tcp"
	dbURL := func(host string, port nat.Port) string {
		return fmt.Sprintf("postgres://postgres:password@%s:%s/%s?sslmode=disable", host, port.Port(), dbname)
	}

	t.Run("default query", func(t *testing.T) {
		ctr, err := postgres.Run(
			ctx,
			"postgres:16-alpine",
			postgres.WithDatabase(dbname),
			postgres.WithUsername(user),
			postgres.WithPassword(password),
			testcontainers.WithWaitStrategy(wait.ForSQL(nat.Port(port), "postgres", dbURL)),
		)
		testcontainers.CleanupContainer(t, ctr)
		require.NoError(t, err)
		require.NotNil(t, ctr)
	})
	t.Run("custom query", func(t *testing.T) {
		ctr, err := postgres.Run(
			ctx,
			"postgres:16-alpine",
			postgres.WithDatabase(dbname),
			postgres.WithUsername(user),
			postgres.WithPassword(password),
			testcontainers.WithWaitStrategy(wait.ForSQL(nat.Port(port), "postgres", dbURL).WithStartupTimeout(time.Second*5).WithQuery("SELECT 10")),
		)
		testcontainers.CleanupContainer(t, ctr)
		require.NoError(t, err)
		require.NotNil(t, ctr)
	})
	t.Run("custom bad query", func(t *testing.T) {
		ctr, err := postgres.Run(
			ctx,
			"postgres:16-alpine",
			postgres.WithDatabase(dbname),
			postgres.WithUsername(user),
			postgres.WithPassword(password),
			testcontainers.WithWaitStrategy(wait.ForSQL(nat.Port(port), "postgres", dbURL).WithStartupTimeout(time.Second*5).WithQuery("SELECT 'a' from b")),
		)
		testcontainers.CleanupContainer(t, ctr)
		require.Error(t, err)
	})
}

func TestWithConfigFile(t *testing.T) {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithConfigFile(filepath.Join("testdata", "my-postgres.conf")),
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	// explicitly set sslmode=disable because the container is not configured to use TLS
	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()
}

func TestWithSSL(t *testing.T) {
	ctx := context.Background()

	caCert, serverCerts, err := createSSLCerts(t)
	require.NoError(t, err)

	ctr, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithConfigFile(filepath.Join("testdata", "postgres-ssl.conf")),
		postgres.WithInitScripts(filepath.Join("testdata", "init-user-db.sh")),
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
		postgres.WithSSLCert(caCert.CertPath, serverCerts.CertPath, serverCerts.KeyPath),
	)

	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	connStr, err := ctr.ConnectionString(ctx, "sslmode=require")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)
	assert.NotNil(t, db)
	defer db.Close()

	result, err := db.Exec("SELECT * FROM testdb;")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestSSLValidatesKeyMaterialPath(t *testing.T) {
	ctx := context.Background()

	_, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithConfigFile(filepath.Join("testdata", "postgres-ssl.conf")),
		postgres.WithInitScripts(filepath.Join("testdata", "init-user-db.sh")),
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(5*time.Second)),
		postgres.WithSSLCert("", "", ""),
	)

	require.Error(t, err, "Error should not have been nil. Container creation should have failed due to empty key material")
}

func TestWithInitScript(t *testing.T) {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:15.2-alpine",
		postgres.WithInitScripts(filepath.Join("testdata", "init-user-db.sh")),
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	// explicitly set sslmode=disable because the container is not configured to use TLS
	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	// database created in init script. See testdata/init-user-db.sh
	result, err := db.Exec("SELECT * FROM testdb;")
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestWithOrderedInitScript(t *testing.T) {
	ctx := context.Background()

	ctr, err := postgres.Run(ctx,
		"postgres:15.2-alpine",
		// Executes first the init-user-db shell-script, then the do-insert-user SQL script
		// Using WithInitScripts, this would not work.
		// This is because aaaa-insert-user would get executed first, but requires init-user-db to be executed before.
		postgres.WithOrderedInitScripts(
			filepath.Join("testdata", "init-user-db.sh"),
			filepath.Join("testdata", "aaaa-insert-user.sql"),
		),
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	// Test that init scripts have been correctly renamed
	c, reader, err := ctr.Exec(ctx, []string{"ls", "-l", "/docker-entrypoint-initdb.d"}, tcexec.Multiplexed())
	require.NoError(t, err)
	require.Equal(t, 0, c, "Expected to read init scripts from the container")

	buf := new(strings.Builder)
	_, err = io.Copy(buf, reader)
	require.NoError(t, err)

	initScripts := buf.String()
	strings.Contains(initScripts, "000-init-user-db.sh")
	strings.Contains(initScripts, "001-aaaa-insert-user.sql")

	// explicitly set sslmode=disable because the container is not configured to use TLS
	connStr, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)
	require.NotNil(t, db)
	defer db.Close()

	// database created in init script. See testdata/init-user-db.sh
	rows, err := db.Query("SELECT COUNT(*) FROM testdb;")
	require.NoError(t, err)
	require.NotNil(t, rows)
	for rows.Next() {
		var count int
		err := rows.Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 2, count)
	}
}

func TestSnapshot(t *testing.T) {
	tests := []struct {
		name    string
		options []postgres.SnapshotOption
	}{
		{
			name:    "snapshot/default",
			options: nil,
		},

		{
			name: "snapshot/custom",
			options: []postgres.SnapshotOption{
				postgres.WithSnapshotName("custom-snapshot"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// snapshotAndReset {
			ctx := context.Background()

			// 1. Start the postgres ctr and run any migrations on it
			ctr, err := postgres.Run(
				ctx,
				"postgres:16-alpine",
				postgres.WithDatabase(dbname),
				postgres.WithUsername(user),
				postgres.WithPassword(password),
				postgres.BasicWaitStrategies(),
				postgres.WithSQLDriver("pgx"),
			)
			testcontainers.CleanupContainer(t, ctr)
			require.NoError(t, err)

			// Run any migrations on the database
			_, _, err = ctr.Exec(ctx, []string{"psql", "-U", user, "-d", dbname, "-c", "CREATE TABLE users (id SERIAL, name TEXT NOT NULL, age INT NOT NULL)"})
			require.NoError(t, err)

			// 2. Create a snapshot of the database to restore later
			// tt.options comes the test case, it can be specified as e.g. `postgres.WithSnapshotName("custom-snapshot")` or omitted, to use default name
			err = ctr.Snapshot(ctx, tt.options...)
			require.NoError(t, err)

			dbURL, err := ctr.ConnectionString(ctx)
			require.NoError(t, err)

			t.Run("Test inserting a user", func(t *testing.T) {
				t.Cleanup(func() {
					// 3. In each test, reset the DB to its snapshot state.
					err = ctr.Restore(ctx)
					require.NoError(t, err)
				})

				conn, err := pgx.Connect(context.Background(), dbURL)
				require.NoError(t, err)
				defer conn.Close(context.Background())

				_, err = conn.Exec(ctx, "INSERT INTO users(name, age) VALUES ($1, $2)", "test", 42)
				require.NoError(t, err)

				var name string
				var age int64
				err = conn.QueryRow(context.Background(), "SELECT name, age FROM users LIMIT 1").Scan(&name, &age)
				require.NoError(t, err)

				require.Equal(t, "test", name)
				require.EqualValues(t, 42, age)
			})

			// 4. Run as many tests as you need, they will each get a clean database
			t.Run("Test querying empty DB", func(t *testing.T) {
				t.Cleanup(func() {
					err = ctr.Restore(ctx)
					require.NoError(t, err)
				})

				conn, err := pgx.Connect(context.Background(), dbURL)
				require.NoError(t, err)
				defer conn.Close(context.Background())

				var name string
				var age int64
				err = conn.QueryRow(context.Background(), "SELECT name, age FROM users LIMIT 1").Scan(&name, &age)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			})
			// }
		})
	}
}

func TestSnapshotWithOverrides(t *testing.T) {
	ctx := context.Background()

	dbname := "other-db"
	user := "other-user"
	password := "other-password"

	ctr, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	_, _, err = ctr.Exec(ctx, []string{"psql", "-U", user, "-d", dbname, "-c", "CREATE TABLE users (id SERIAL, name TEXT NOT NULL, age INT NOT NULL)"})
	require.NoError(t, err)
	err = ctr.Snapshot(ctx, postgres.WithSnapshotName("other-snapshot"))
	require.NoError(t, err)

	dbURL, err := ctr.ConnectionString(ctx)
	require.NoError(t, err)

	t.Run("Test that the restore works when not using defaults", func(t *testing.T) {
		_, _, err = ctr.Exec(ctx, []string{"psql", "-U", user, "-d", dbname, "-c", "INSERT INTO users(name, age) VALUES ('test', 42)"})
		require.NoError(t, err)

		// Doing the restore before we connect since this resets the pgx connection
		err = ctr.Restore(ctx)
		require.NoError(t, err)

		conn, err := pgx.Connect(context.Background(), dbURL)
		require.NoError(t, err)
		defer conn.Close(context.Background())

		var count int64
		err = conn.QueryRow(context.Background(), "SELECT COUNT(1) FROM users").Scan(&count)
		require.NoError(t, err)

		require.Zero(t, count)
	})
}

func TestSnapshotDuplicate(t *testing.T) {
	ctx := context.Background()

	dbname := "other-db"
	user := "other-user"
	password := "other-password"

	ctr, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	_, _, err = ctr.Exec(ctx, []string{"psql", "-U", user, "-d", dbname, "-c", "CREATE TABLE users (id SERIAL, name TEXT NOT NULL, age INT NOT NULL)"})
	require.NoError(t, err)

	err = ctr.Snapshot(ctx, postgres.WithSnapshotName("other-snapshot"))
	require.NoError(t, err)

	err = ctr.Snapshot(ctx, postgres.WithSnapshotName("other-snapshot"))
	require.NoError(t, err)
}

func TestSnapshotWithDockerExecFallback(t *testing.T) {
	ctx := context.Background()

	// postgresWithSQLDriver {
	// 1. Start the postgres container and run any migrations on it
	ctr, err := postgres.Run(
		ctx,
		"postgres:16-alpine",
		postgres.WithDatabase(dbname),
		postgres.WithUsername(user),
		postgres.WithPassword(password),
		postgres.BasicWaitStrategies(),
		// Tell the postgres module to use a driver that doesn't exist
		// This will cause the module to fall back to using docker exec
		postgres.WithSQLDriver("DoesNotExist"),
	)
	// }
	testcontainers.CleanupContainer(t, ctr)
	require.NoError(t, err)

	// Run any migrations on the database
	_, _, err = ctr.Exec(ctx, []string{"psql", "-U", user, "-d", dbname, "-c", "CREATE TABLE users (id SERIAL, name TEXT NOT NULL, age INT NOT NULL)"})
	require.NoError(t, err)

	// 2. Create a snapshot of the database to restore later
	err = ctr.Snapshot(ctx, postgres.WithSnapshotName("test-snapshot"))
	require.NoError(t, err)

	dbURL, err := ctr.ConnectionString(ctx)
	require.NoError(t, err)

	t.Run("Test inserting a user", func(t *testing.T) {
		t.Cleanup(func() {
			// 3. In each test, reset the DB to its snapshot state.
			err := ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err2 := pgx.Connect(context.Background(), dbURL)
		require.NoError(t, err2)
		defer conn.Close(context.Background())

		_, err2 = conn.Exec(ctx, "INSERT INTO users(name, age) VALUES ($1, $2)", "test", 42)
		require.NoError(t, err2)

		var name string
		var age int64
		err2 = conn.QueryRow(context.Background(), "SELECT name, age FROM users LIMIT 1").Scan(&name, &age)
		require.NoError(t, err2)

		require.Equal(t, "test", name)
		require.EqualValues(t, 42, age)
	})

	t.Run("Test querying empty DB", func(t *testing.T) {
		// 4. Run as many tests as you need, they will each get a clean database
		t.Cleanup(func() {
			err := ctr.Restore(ctx)
			require.NoError(t, err)
		})

		conn, err2 := pgx.Connect(context.Background(), dbURL)
		require.NoError(t, err2)
		defer conn.Close(context.Background())

		var name string
		var age int64
		err2 = conn.QueryRow(context.Background(), "SELECT name, age FROM users LIMIT 1").Scan(&name, &age)
		require.ErrorIs(t, err2, pgx.ErrNoRows)
	})
	// }
}
