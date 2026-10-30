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
				require.NoError(t, db.Migrator().DropTable(&ChannelCircuit{}))
				DB, common.ChannelCircuitFailureThreshold = previousDB, previousThreshold
				channelCircuits = map[int]ChannelCircuit{}
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.Migrator().DropTable(&ChannelCircuit{}))
			require.NoError(t, db.AutoMigrate(&ChannelCircuit{}))
			require.NoError(t, db.AutoMigrate(&ChannelCircuit{}), "the migration can run again")
			var version string
			query := "SELECT version()"
			if dialect == "sqlite" {
				query = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(query).Scan(&version).Error)
			t.Logf("database version: %s", version)

			// The master and the edge share the table but keep their own copies;
			// switching instances swaps the copy this package reads.
			copies := map[string]map[int]ChannelCircuit{"master": {}, "edge": {}}
			current := "master"
			channelCircuits = copies[current]
			on := func(instance string) {
				copies[current] = channelCircuits
				current = instance
				channelCircuits = copies[instance]
			}
			stored := func(channelID int) ChannelCircuit {
				var circuit ChannelCircuit
				require.NoError(t, db.Where("channel_id = ?", channelID).Find(&circuit).Error)
				return circuit
			}

			assert.False(t, RecordChannelFailure(1, "first"))
			assert.False(t, RecordChannelFailure(1, "second"))
			on("edge")
			assert.True(t, RecordChannelFailure(1, "third: 上游超时"), "failures on either instance count toward the same run")
			assert.True(t, IsChannelCircuitOpen(1))
			opened := stored(1)
			assert.Equal(t, 3, opened.Failures)
			assert.Equal(t, "third: 上游超时", opened.LastFailure)
			assert.Positive(t, opened.OpenedAt)

			on("master")
			assert.False(t, IsChannelCircuitOpen(1), "the master has not synced yet")
			require.NoError(t, loadChannelCircuits())
			assert.True(t, IsChannelCircuitOpen(1))
			assert.Equal(t, []ChannelCircuit{opened}, OpenChannelCircuits())
			assert.False(t, RecordChannelFailure(1, "fourth"), "an open circuit opens only once")
			assert.Equal(t, 4, stored(1).Failures)

			assert.True(t, ClaimChannelCircuitProbe(1, 0))
			on("edge")
			assert.False(t, ClaimChannelCircuitProbe(1, 0), "one instance tests the channel each round")
			assert.True(t, RecordChannelSuccess(1), "a success on either instance closes the circuit")
			on("master")
			require.NoError(t, loadChannelCircuits())
			assert.False(t, IsChannelCircuitOpen(1))
			assert.Empty(t, OpenChannelCircuits())
			assert.False(t, RecordChannelSuccess(1))

			assert.False(t, RecordChannelFailure(2, "first"))
			assert.False(t, RecordChannelFailure(2, "second"))
			on("edge")
			require.NoError(t, loadChannelCircuits())
			assert.False(t, RecordChannelSuccess(2), "the run was not open")
			on("master")
			assert.False(t, RecordChannelFailure(2, "after a success elsewhere"), "a success on either instance restarts the run")
			assert.Equal(t, 1, stored(2).Failures)

			if connections > 1 {
				var wait sync.WaitGroup
				var openers atomic.Int32
				for range 4 {
					wait.Go(func() {
						if RecordChannelFailure(3, "concurrent") {
							openers.Add(1)
						}
					})
				}
				wait.Wait()
				assert.EqualValues(t, 1, openers.Load(), "concurrent failures open the circuit once")
				assert.Equal(t, 4, stored(3).Failures)
			}

			common.ChannelCircuitFailureThreshold = 0
			assert.Empty(t, OpenChannelCircuits())
			require.NoError(t, loadChannelCircuits())
			var remaining int64
			require.NoError(t, db.Model(&ChannelCircuit{}).Count(&remaining).Error)
			assert.Zero(t, remaining, "turning the breaker off closes every circuit on every instance")
		})
	}
}
