package model

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestChannelCircuitsAreSharedByInstances(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			connections := 4
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
				connections = 1
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "circuit_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(connections)
			previousDB, previousThreshold := DB, common.ChannelCircuitFailureThreshold
			DB = db
			common.ChannelCircuitFailureThreshold = 3
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&ChannelModelCircuit{}))
				DB, common.ChannelCircuitFailureThreshold = previousDB, previousThreshold
				channelCircuits = map[channelModelKey]ChannelModelCircuit{}
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.Migrator().DropTable(&ChannelModelCircuit{}))
			require.NoError(t, db.AutoMigrate(&ChannelModelCircuit{}))
			require.NoError(t, db.AutoMigrate(&ChannelModelCircuit{}), "the migration can run again")
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// The master and the edge share the table but keep their own copies;
			// switching instances swaps the copy this package reads.
			copies := map[string]map[channelModelKey]ChannelModelCircuit{"master": {}, "edge": {}}
			current := "master"
			channelCircuits = copies[current]
			on := func(instance string) {
				copies[current] = channelCircuits
				current = instance
				channelCircuits = copies[instance]
			}
			stored := func(channelID int, modelName string) ChannelModelCircuit {
				var circuit ChannelModelCircuit
				require.NoError(t, db.Where("channel_id = ? AND model = ?", channelID, modelName).Find(&circuit).Error)
				return circuit
			}
			const fable, opus = "claude-fable-5-1", "claude-opus-5-5"

			assert.False(t, RecordChannelFailure(1, fable, "first"))
			assert.False(t, RecordChannelFailure(1, fable, "second"))
			assert.False(t, RecordChannelFailure(1, opus, "another model"), "each model of a channel has its own run")
			on("edge")
			assert.True(t, RecordChannelFailure(1, fable, "third: 上游没有这个模型"), "failures on either instance count toward the same run")
			assert.True(t, IsChannelCircuitOpen(1, fable))
			assert.False(t, IsChannelCircuitOpen(1, opus), "the channel keeps serving its other models")
			opened := stored(1, fable)
			assert.Equal(t, 3, opened.Failures)
			assert.Equal(t, "third: 上游没有这个模型", opened.LastFailure)
			assert.Positive(t, opened.OpenedAt)

			on("master")
			assert.False(t, IsChannelCircuitOpen(1, fable), "the master has not synced yet")
			require.NoError(t, loadChannelCircuits())
			assert.True(t, IsChannelCircuitOpen(1, fable))
			assert.Equal(t, []ChannelModelCircuit{opened}, OpenChannelCircuits())
			assert.False(t, RecordChannelFailure(1, fable, "fourth"), "an open circuit opens only once")
			assert.Equal(t, 4, stored(1, fable).Failures)
			assert.False(t, RecordChannelSuccess(1, opus), "the other model's run was not open")
			assert.True(t, IsChannelCircuitOpen(1, fable), "a success with another model leaves the circuit open")

			assert.True(t, ClaimChannelCircuitProbe(1, fable, 0))
			on("edge")
			assert.False(t, ClaimChannelCircuitProbe(1, fable, 0), "one instance tests the channel each round")
			assert.True(t, RecordChannelSuccess(1, fable), "a success on either instance closes the circuit")
			on("master")
			require.NoError(t, loadChannelCircuits())
			assert.False(t, IsChannelCircuitOpen(1, fable))
			assert.Empty(t, OpenChannelCircuits())
			assert.False(t, RecordChannelSuccess(1, fable))

			assert.False(t, RecordChannelFailure(2, fable, "first"))
			assert.False(t, RecordChannelFailure(2, fable, "second"))
			on("edge")
			require.NoError(t, loadChannelCircuits())
			assert.False(t, RecordChannelSuccess(2, fable), "the run was not open")
			on("master")
			assert.False(t, RecordChannelFailure(2, fable, "after a success elsewhere"), "a success on either instance restarts the run")
			assert.Equal(t, 1, stored(2, fable).Failures)

			if connections > 1 {
				var wait sync.WaitGroup
				var openers atomic.Int32
				for range 4 {
					wait.Go(func() {
						if RecordChannelFailure(3, fable, "concurrent") {
							openers.Add(1)
						}
					})
				}
				wait.Wait()
				assert.EqualValues(t, 1, openers.Load(), "concurrent failures open the circuit once")
				assert.Equal(t, 4, stored(3, fable).Failures)
			}

			common.ChannelCircuitFailureThreshold = 0
			assert.Empty(t, OpenChannelCircuits())
			require.NoError(t, loadChannelCircuits())
			var remaining int64
			require.NoError(t, db.Model(&ChannelModelCircuit{}).Count(&remaining).Error)
			assert.Zero(t, remaining, "turning the breaker off closes every circuit on every instance")
		})
	}
}
