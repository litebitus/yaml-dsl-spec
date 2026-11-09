package yamldslspec

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sync"
)

const modulePath = "github.com/litebitus/yaml-dsl-spec/go"

// SchemaStore keeps, across runs, the information a schema cache prepares from a schema's text, by the key the cache names: the caller's to place.
type SchemaStore interface {
	// Read is the prepared information written under storeKey, and whether the store holds it.
	Read(storeKey string) (string, bool)
	Write(storeKey, preparedText string)
}

// SchemaCacheOption sets how a schema cache holds and keeps the schemas it loads.
type SchemaCacheOption func(*SchemaCache)

// WithCapacity holds at most heldCapacity schemas, the least recently asked for dropped first; it panics below one.
func WithCapacity(heldCapacity int) SchemaCacheOption {
	if heldCapacity < 1 {
		panic(fmt.Sprintf("a schema cache's capacity is a whole number from 1, not %d", heldCapacity))
	}
	return func(schemaCache *SchemaCache) { schemaCache.heldCapacity = heldCapacity }
}

// WithStore keeps what the cache prepares from each schema in schemaStore, and reads it from there before preparing a schema again.
func WithStore(schemaStore SchemaStore) SchemaCacheOption {
	return func(schemaCache *SchemaCache) { schemaCache.schemaStore = schemaStore }
}

// SchemaCache holds schemas loaded once per name and text, shared by every reader that asks for them; without a capacity, every schema loaded is held.
type SchemaCache struct {
	cacheLock    sync.Mutex
	heldSchemas  map[schemaCacheKey]*heldSchema
	askedTurn    uint64
	heldCapacity int
	schemaStore  SchemaStore
}

type schemaCacheKey struct {
	schemaName string
	schemaText string
}

type heldSchema struct {
	pendingLoad *schemaLoad
	lastAsked   uint64
}

type schemaLoad struct {
	loadOnce     sync.Once
	loadedSchema *LoadedSchema
	loadError    error
}

// NewSchemaCache is an empty schema cache, set by cacheOptions.
func NewSchemaCache(cacheOptions ...SchemaCacheOption) *SchemaCache {
	schemaCache := &SchemaCache{heldSchemas: map[schemaCacheKey]*heldSchema{}}
	for _, cacheOption := range cacheOptions {
		cacheOption(schemaCache)
	}
	return schemaCache
}

// Load is the schema a text holds, named as reports name it, read once per name and text; else why it does not load.
func (schemaCache *SchemaCache) Load(schemaName, schemaText string) (*LoadedSchema, error) {
	schemaCache.cacheLock.Lock()
	if schemaCache.heldSchemas == nil {
		schemaCache.heldSchemas = map[schemaCacheKey]*heldSchema{}
	}
	schemaCache.askedTurn++
	cacheKey := schemaCacheKey{schemaName: schemaName, schemaText: schemaText}
	askedSchema, isHeld := schemaCache.heldSchemas[cacheKey]
	if !isHeld {
		askedSchema = &heldSchema{pendingLoad: &schemaLoad{}}
		schemaCache.heldSchemas[cacheKey] = askedSchema
	}
	askedSchema.lastAsked = schemaCache.askedTurn
	schemaCache.dropOldestPastCapacity(cacheKey)
	schemaCache.cacheLock.Unlock()
	pendingLoad := askedSchema.pendingLoad
	pendingLoad.loadOnce.Do(func() {
		pendingLoad.loadedSchema, pendingLoad.loadError = schemaCache.preparedLoad(schemaName, schemaText)
	})
	return pendingLoad.loadedSchema, pendingLoad.loadError
}

func (schemaCache *SchemaCache) dropOldestPastCapacity(askedKey schemaCacheKey) {
	if schemaCache.heldCapacity == 0 {
		return
	}
	for len(schemaCache.heldSchemas) > schemaCache.heldCapacity {
		var oldestKey schemaCacheKey
		var oldestTurn uint64
		foundOldest := false
		for heldKey, held := range schemaCache.heldSchemas {
			if heldKey != askedKey && (!foundOldest || held.lastAsked < oldestTurn) {
				oldestKey, oldestTurn, foundOldest = heldKey, held.lastAsked, true
			}
		}
		if !foundOldest {
			return
		}
		delete(schemaCache.heldSchemas, oldestKey)
	}
}

// preparedLoad is the schema built from the information the store holds for it, else from its text, the information prepared then written to the store.
func (schemaCache *SchemaCache) preparedLoad(schemaName, schemaText string) (*LoadedSchema, error) {
	if schemaCache.schemaStore == nil {
		return parseLoadedSchema(schemaName, schemaText)
	}
	storeKey := schemaStoreKey(schemaName, schemaText)
	if storedText, isStored := schemaCache.schemaStore.Read(storeKey); isStored {
		if storedPreparation, readError := preparedSchemaOfText(storedText); readError == nil {
			if restoredSchema, buildError := loadedSchemaFromPrepared(schemaName, storedPreparation); buildError == nil {
				return restoredSchema, nil
			}
		}
	}
	schemaPreparation, preparationError := preparedSchemaOf(schemaName, schemaText)
	if preparationError != nil {
		return nil, preparationError
	}
	schemaCache.schemaStore.Write(storeKey, schemaPreparation.preparedText())
	return loadedSchemaFromPrepared(schemaName, schemaPreparation)
}

// schemaStoreKey is the key a schema's prepared information is stored under: its name, its text and the version of the module that prepared it, so another version never reads it.
func schemaStoreKey(schemaName, schemaText string) string {
	keyDigest := sha256.New()
	for _, keyPart := range []string{"yaml-dsl-spec", moduleVersion(), schemaName, schemaText} {
		keyDigest.Write([]byte(keyPart))
		keyDigest.Write([]byte{0})
	}
	return hex.EncodeToString(keyDigest.Sum(nil))
}

// moduleVersion is the version of this module the build holds, "(devel)" where it is the module being built.
func moduleVersion() string {
	buildInfo, isRead := debug.ReadBuildInfo()
	if !isRead {
		return "(devel)"
	}
	if buildInfo.Main.Path == modulePath {
		return buildInfo.Main.Version
	}
	for _, dependency := range buildInfo.Deps {
		if dependency.Path == modulePath {
			return dependency.Version
		}
	}
	return "(devel)"
}
