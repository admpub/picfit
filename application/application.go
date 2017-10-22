package application

import (
	"fmt"
	"net/url"
	"strings"

	"context"

	"github.com/Sirupsen/logrus"
	"github.com/admpub/picfit/config"
	"github.com/admpub/picfit/engine"
	"github.com/admpub/picfit/errs"
	"github.com/admpub/picfit/hash"
	"github.com/admpub/picfit/image"
	"github.com/admpub/picfit/kvstore"
	"github.com/admpub/picfit/logger"
	"github.com/admpub/picfit/storage"
	"github.com/gin-gonic/gin"
)

// LoadFromConfigContent returns a net/context from content
func LoadFromConfigContent(content string) (context.Context, error) {
	cfg, err := config.LoadFromContent(content)

	if err != nil {
		return nil, err
	}

	return LoadFromConfig(cfg)
}

// LoadFromConfig returns a net/context from a config.Config instance
func LoadFromConfig(cfg *config.Config) (context.Context, error) {
	ctx := config.NewContext(context.Background(), *cfg)

	sourceStorage, destinationStorage, err := storage.NewStoragesFromConfig(cfg)

	if err != nil {
		return nil, err
	}

	ctx = storage.NewSourceContext(ctx, sourceStorage)
	ctx = storage.NewDestinationContext(ctx, destinationStorage)

	keystore, err := kvstore.NewKVStoreFromConfig(cfg)

	if err != nil {
		return nil, err
	}

	ctx = kvstore.NewContext(ctx, keystore)

	e := &engine.GoImageEngine{
		DefaultFormat:  cfg.Options.DefaultFormat,
		Format:         cfg.Options.Format,
		DefaultQuality: cfg.Options.Quality,
	}

	log := logrus.New()
	level, err := logrus.ParseLevel(cfg.Logger.GetLevel())
	if err != nil {
		return nil, err
	}
	log.Level = level

	ctx = engine.NewContext(ctx, e)
	ctx = logger.NewContext(ctx, log)

	return ctx, nil
}

// Load creates a net/context from a file config path
func Load(path string, port int) (context.Context, error) {
	cfg, err := config.Load(path)

	if err != nil {
		return nil, err
	}
	if port > 0 {
		cfg.Port = port
	}
	//cfg.Options.EnableDelete = true
	return LoadFromConfig(cfg)
}

// Store stores an image file with the defined filepath
func Store(ctx context.Context, filepath string, i *image.ImageFile) error {
	l := logger.FromContext(ctx)

	cfg := config.FromContext(ctx)

	con := kvstore.FromContext(ctx)

	err := i.Save()

	if err != nil {
		l.Fatal(err)
		return err
	}

	l.Infof("Save thumbnail %s to storage", i.Filepath)

	prefix := cfg.KVStore.Prefix

	storeKey := i.Key

	key := i.Key

	if prefix != "" {
		storeKey = prefix + storeKey
	}

	err = con.Set(storeKey, i.Filepath)

	if err != nil {
		l.Fatal(err)

		return err
	}

	l.Infof("Save key %s => %s to kvstore", storeKey, i.Filepath)

	// Write children info only when we actually want to be able to delete things.
	if cfg.Options.EnableDelete {
		parentKey := hash.Tokey(filepath)

		if prefix != "" {
			parentKey = prefix + parentKey
		}

		parentKey = fmt.Sprintf("%s:children", parentKey)
		err = con.AppendSlice(parentKey, storeKey)

		if err != nil {
			l.Fatal(err)
			return err
		}

		l.Infof("Put key into set %s (%s) => %s in kvstore", parentKey, filepath, key)
	}

	return nil
}

// Delete removes a file from kvstore and storage
func Delete(ctx context.Context, filepath string) error {

	con := kvstore.FromContext(ctx)

	l := logger.FromContext(ctx)

	l.Infof("Deleting source storage file: %s", filepath)

	sourceStorage := storage.SourceFromContext(ctx)

	if !sourceStorage.Exists(filepath) {
		l.Infof("File %s does not exist anymore on source storage", filepath)

		return errs.ErrFileNotExists
	}

	err := sourceStorage.Delete(filepath)

	if err != nil {
		return err
	}

	parentKey := hash.Tokey(filepath)

	prefix := config.FromContext(ctx).KVStore.Prefix

	if prefix != "" {
		parentKey = prefix + parentKey
	}

	childrenKey := fmt.Sprintf("%s:children", parentKey)

	if ok, _ := con.Exists(childrenKey); !ok {
		l.Infof("Children key %s does not exist for parent %s", childrenKey, parentKey)

		return errs.ErrKeyNotExists
	}

	// Get the list of items to cleanup.
	children, _ := con.GetSlice(childrenKey)

	if children == nil {
		l.Infof("No children to delete for %s", parentKey)

		return nil
	}

	store := storage.DestinationFromContext(ctx)

	for _, _key := range children {
		key, _ := _key.(string)
		// Now, every child is a hash which points to a key/value pair in
		// KVStore which in turn points to a file in dst storage.
		v, err := con.Get(key)
		if err != nil {
			return err
		}
		dstfile, _ := v.(string)

		if err != nil {
			return err
		}

		// And try to delete it all.
		err = store.Delete(dstfile)

		if err != nil {
			return err
		}

		err = con.Delete(key)

		if err != nil {
			return err
		}

		l.Infof("Deleting child %s and its entry %s", dstfile, key)
	}

	// Delete them right away, we don't care about them anymore.
	l.Infof("Deleting children set %s", childrenKey)

	err = con.Delete(childrenKey)

	if err != nil {
		return err
	}

	return nil
}

// ImageFileFromContext generates an ImageFile from gin context
func ImageFileFromContext(c *gin.Context, async bool, load bool) (*image.ImageFile, error) {
	key := c.MustGet("key").(string)

	con := kvstore.FromContext(c)

	cfg := config.FromContext(c)

	l := logger.FromContext(c)

	destStorage := storage.DestinationFromContext(c)

	var file = &image.ImageFile{
		Key:     key,
		Storage: destStorage,
		Headers: map[string]string{},
	}
	var err error
	var filepath string

	prefix := cfg.KVStore.Prefix

	storeKey := key

	if prefix != "" {
		storeKey = prefix + key
	}

	// Image from the KVStore found
	v, err := con.Get(storeKey)
	if err != nil {
		return nil, err
	}
	stored, _ := v.(string)

	file.Filepath = stored

	if stored != "" {
		l.Infof("Key %s found in kvstore: %s", storeKey, stored)

		noneMatch := c.Request.Header.Get("If-None-Match")
		if noneMatch == key {
			return nil, errs.ErrClientHasImage
		}

		if load {
			file, err = image.FromStorage(destStorage, stored)

			if err != nil {
				return nil, err
			}
		}
	} else {
		l.Infof("Key %s not found in kvstore", storeKey)

		u, exists := c.Get("url")

		parameters := c.MustGet("parameters").(map[string]string)

		// Image not found from the KVStore, we need to process it
		// URL available in Query String
		if exists {
			file, err = image.FromURL(u.(*url.URL), cfg.Options.DefaultUserAgent)
		} else {
			// URL provided we use http protocol to retrieve it
			s := storage.SourceFromContext(c)

			filepath = parameters["path"]

			if !s.Exists(filepath) {
				return nil, errs.ErrFileNotExists
			}

			file, err = image.FromStorage(s, filepath)
		}

		if err != nil {
			return nil, err
		}

		op := c.MustGet("op").(*engine.Operation)

		file, err = engine.FromContext(c).Transform(file, op, parameters)

		if err != nil {
			return nil, err
		}

		filename := ShardFilename(c, key)

		file.Filepath = fmt.Sprintf("%s.%s", filename, file.Format())
	}

	file.Key = key
	file.Storage = destStorage

	file.Headers["ETag"] = key
	file.Headers["Cache-Control"] = fmt.Sprintf("max-age=%d, s-maxage=%d", cfg.CacheControl, cfg.CacheControl)

	if stored == "" {
		if async == true {
			go Store(c, filepath, file)
		} else {
			err = Store(c, filepath, file)
		}
	}

	return file, err
}

// ShardFilename shards a filename based on config
func ShardFilename(ctx context.Context, filename string) string {
	cfg := config.FromContext(ctx)

	results := hash.Shard(filename, cfg.Shard.Width, cfg.Shard.Depth, cfg.Shard.RestOnly)

	return strings.Join(results, "/")
}
