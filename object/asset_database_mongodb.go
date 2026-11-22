// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// mongodbDriver connects to the server of the asset directly, without following
// the members of a replica set, whose addresses the gateway may not reach.
type mongodbDriver struct {
	c      *DatabaseClient
	client *mongo.Client
}

func (d *mongodbDriver) connect() error {
	err := d.connectWithAuthSource("admin")
	database := d.c.Asset.Database
	// a user created in the database of the asset rather than in admin
	if err != nil && d.c.Asset.Username != "" && database != "" && database != "admin" && isMongoAuthError(err) {
		d.close()
		err = d.connectWithAuthSource(database)
	}
	return err
}

func isMongoAuthError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "auth") || strings.Contains(text, "credential")
}

func (d *mongodbDriver) connectWithAuthSource(authSource string) error {
	c := d.c
	opts := options.Client().
		SetHosts([]string{net.JoinHostPort(c.Asset.Host, strconv.Itoa(c.Asset.Port))}).
		SetDirect(true).
		SetDialer(databaseDialer{dial: c.dialTls}).
		SetAppName("casvisor").
		SetConnectTimeout(2 * dialTimeout).
		SetServerSelectionTimeout(2 * dialTimeout).
		SetRetryReads(false).
		SetRetryWrites(false).
		SetMaxPoolSize(1)
	if c.Asset.Username != "" {
		opts.SetAuth(options.Credential{Username: c.Asset.Username, Password: c.Asset.Password, AuthSource: authSource})
	}

	client, err := mongo.Connect(opts)
	if err != nil {
		return err
	}
	d.client = client

	ctx, cancel := context.WithTimeout(c.ctx, 2*dialTimeout)
	defer cancel()
	return client.Ping(ctx, nil)
}

func (d *mongodbDriver) close() {
	if d.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.client.Disconnect(ctx)
		d.client = nil
	}
}

func (d *mongodbDriver) getVersionAndDatabase() (string, string, error) {
	var info struct {
		Version string `bson:"version"`
	}
	err := d.client.Database("admin").RunCommand(d.c.ctx, bson.D{{Key: "buildInfo", Value: 1}}).Decode(&info)
	if err != nil {
		return "", "", err
	}

	database := d.c.Asset.Database
	if database == "" {
		database = "test"
	}
	return info.Version, database, nil
}

func (d *mongodbDriver) getDatabases() ([]string, error) {
	names, err := d.client.ListDatabaseNames(d.c.ctx, bson.D{}, options.ListDatabases().SetAuthorizedDatabases(true))
	if err != nil {
		// a user that may not list the databases still sees its own
		return []string{}, nil
	}
	sort.Strings(names)
	return names, nil
}

func (d *mongodbDriver) getTables(database string) ([]*DatabaseTable, error) {
	specs, err := d.client.Database(database).ListCollectionSpecifications(d.c.ctx, bson.D{}, options.ListCollections().SetAuthorizedCollections(true))
	if err != nil {
		return nil, err
	}

	res := []*DatabaseTable{}
	for _, spec := range specs {
		res = append(res, &DatabaseTable{Name: spec.Name, Type: spec.Type})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Name < res[j].Name })
	return res, nil
}

// getColumns lists the fields of a document of the collection, with their types.
func (d *mongodbDriver) getColumns(database string, schema string, table string) ([]*DatabaseColumn, error) {
	raw, err := d.client.Database(database).Collection(table).FindOne(d.c.ctx, bson.D{}).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return []*DatabaseColumn{}, nil
	}
	if err != nil {
		return nil, err
	}
	elements, err := raw.Elements()
	if err != nil {
		return nil, err
	}

	res := []*DatabaseColumn{}
	for _, element := range elements {
		column := &DatabaseColumn{Name: element.Key(), Type: mongoTypeNames[element.Value().Type], Nullable: true}
		if column.Name == "_id" {
			column.Key = "PRI"
			column.Nullable = false
		}
		res = append(res, column)
	}
	return res, nil
}

func (d *mongodbDriver) getUseStatement(database string) string {
	return "use " + database
}

func (d *mongodbDriver) useDatabase(database string) error {
	if database == "" {
		return fmt.Errorf("the database name is empty")
	}
	return nil
}

func getMongoDocument(args []interface{}, i int, what string) (bson.D, error) {
	if i >= len(args) || args[i] == nil {
		return bson.D{}, nil
	}
	doc, ok := args[i].(bson.D)
	if !ok {
		return nil, fmt.Errorf("the %s should be a document such as {}", what)
	}
	return doc, nil
}

func getMongoInt(args []interface{}, what string) (int64, error) {
	if len(args) != 1 {
		return 0, fmt.Errorf("%s() takes a number", what)
	}
	n, ok := toMongoInt(args[0])
	if !ok {
		return 0, fmt.Errorf("%s() takes a number", what)
	}
	return n, nil
}

func getMongoBool(doc bson.D, key string) bool {
	for _, e := range doc {
		if e.Key == key {
			b, _ := e.Value.(bool)
			return b
		}
	}
	return false
}

// addMongoDocuments shows documents as rows, with a column for each top-level field.
func addMongoDocuments(res *SqlResult, docs []bson.Raw) {
	res.Columns = []SqlColumn{}
	res.Rows = [][]*string{}
	index := map[string]int{}
	rows := []map[int]*string{}
	for _, doc := range docs {
		elements, err := doc.Elements()
		if err != nil {
			continue
		}
		row := map[int]*string{}
		for _, element := range elements {
			key := element.Key()
			value := element.Value()
			i, ok := index[key]
			if !ok {
				i = len(res.Columns)
				index[key] = i
				res.Columns = append(res.Columns, SqlColumn{Name: key})
			}
			if res.Columns[i].Type == "" && value.Type != bson.TypeNull {
				res.Columns[i].Type = mongoTypeNames[value.Type]
			}
			row[i] = formatMongoValue(value)
		}
		rows = append(rows, row)
	}

	for _, row := range rows {
		cells := make([]*string, len(res.Columns))
		for i, value := range row {
			cells[i] = value
		}
		res.Rows = append(res.Rows, cells)
	}
}

// readMongoCursor reads the documents of a cursor, keeping the first ones and counting all of them.
func readMongoCursor(ctx context.Context, cursor *mongo.Cursor, res *SqlResult) error {
	defer cursor.Close(context.Background())

	docs := []bson.Raw{}
	size := 0
	for cursor.Next(ctx) {
		res.RowCount++
		if res.Truncated {
			continue
		}
		if len(docs) >= SqlMaxRows || size > sqlMaxResultSize {
			res.Truncated = true
			continue
		}
		doc := make(bson.Raw, len(cursor.Current))
		copy(doc, cursor.Current)
		size += len(doc)
		docs = append(docs, doc)
	}
	addMongoDocuments(res, docs)
	return cursor.Err()
}

func addMongoValues(res *SqlResult, name string, values []string) {
	res.Columns = []SqlColumn{{Name: name}}
	res.Rows = [][]*string{}
	for _, value := range values {
		v := value
		res.Rows = append(res.Rows, []*string{&v})
	}
	res.RowCount = int64(len(values))
}

func (d *mongodbDriver) execute(statement string) []*SqlResult {
	call, err := ParseMongoStatement(statement)
	if err != nil {
		return newErrorResult(err)
	}

	res := &SqlResult{}
	err = d.run(call, res)
	if err != nil {
		res.Error = err.Error()
	}
	return []*SqlResult{res}
}

func (d *mongodbDriver) run(call *mongoCall, res *SqlResult) error {
	ctx := d.c.ctx
	switch {
	case call.use != "":
		d.c.Database = call.use
		return nil
	case call.show != "":
		switch call.show {
		case "dbs", "databases":
			result, err := d.client.ListDatabases(ctx, bson.D{}, options.ListDatabases().SetAuthorizedDatabases(true))
			if err != nil {
				return err
			}
			res.Columns = []SqlColumn{{Name: "name"}, {Name: "sizeOnDisk"}, {Name: "empty"}}
			res.Rows = [][]*string{}
			for _, spec := range result.Databases {
				name, size, empty := spec.Name, strconv.FormatInt(spec.SizeOnDisk, 10), strconv.FormatBool(spec.Empty)
				res.Rows = append(res.Rows, []*string{&name, &size, &empty})
			}
			res.RowCount = int64(len(res.Rows))
			return nil
		case "collections", "tables":
			names, err := d.client.Database(d.c.Database).ListCollectionNames(ctx, bson.D{})
			if err != nil {
				return err
			}
			sort.Strings(names)
			addMongoValues(res, "name", names)
			return nil
		}
		return fmt.Errorf("unknown show command: show %s, use show dbs or show collections", call.show)
	case call.db:
		addMongoValues(res, "database", []string{d.c.Database})
		return nil
	}

	database := call.database
	if database == "" {
		database = d.c.Database
	}
	db := d.client.Database(database)
	if call.collection == "" {
		return d.runDatabaseMethod(db, call, res)
	}
	return d.runCollectionMethod(db.Collection(call.collection), call, res)
}

func (d *mongodbDriver) runCommand(db *mongo.Database, command bson.D, res *SqlResult) error {
	raw, err := db.RunCommand(d.c.ctx, command).Raw()
	if err != nil {
		return err
	}
	addMongoDocuments(res, []bson.Raw{raw})
	res.RowCount = 1
	return nil
}

func (d *mongodbDriver) runDatabaseMethod(db *mongo.Database, call *mongoCall, res *SqlResult) error {
	ctx := d.c.ctx
	switch call.method {
	case "runCommand", "adminCommand":
		command, err := getMongoDocument(call.args, 0, "command")
		if err != nil {
			return err
		}
		if len(command) == 0 {
			return fmt.Errorf("%s() takes a command such as {ping: 1}", call.method)
		}
		if call.method == "adminCommand" {
			db = d.client.Database("admin")
		}
		return d.runCommand(db, command, res)
	case "getCollectionNames":
		names, err := db.ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return err
		}
		sort.Strings(names)
		addMongoValues(res, "name", names)
		return nil
	case "getCollectionInfos":
		filter, err := getMongoDocument(call.args, 0, "filter")
		if err != nil {
			return err
		}
		cursor, err := db.ListCollections(ctx, filter)
		if err != nil {
			return err
		}
		return readMongoCursor(ctx, cursor, res)
	case "createCollection":
		name, ok := firstArg(call.args).(string)
		if !ok || name == "" {
			return fmt.Errorf("createCollection() takes a name")
		}
		return db.CreateCollection(ctx, name)
	case "dropDatabase":
		return db.Drop(ctx)
	case "stats":
		return d.runCommand(db, bson.D{{Key: "dbStats", Value: 1}}, res)
	case "serverStatus":
		return d.runCommand(d.client.Database("admin"), bson.D{{Key: "serverStatus", Value: 1}}, res)
	case "version":
		addMongoValues(res, "version", []string{d.c.Version})
		return nil
	case "getName":
		addMongoValues(res, "database", []string{db.Name()})
		return nil
	}
	return fmt.Errorf("the database method: %s() is not supported, use db.runCommand() for other commands", call.method)
}

func (d *mongodbDriver) runCollectionMethod(coll *mongo.Collection, call *mongoCall, res *SqlResult) error {
	ctx := d.c.ctx
	args := call.args
	if call.method != "find" {
		for _, chained := range call.chain {
			if chained.name != "pretty" && chained.name != "toArray" {
				return fmt.Errorf("%s() can't be chained after %s()", chained.name, call.method)
			}
		}
	}

	switch call.method {
	case "find":
		return d.find(coll, call, res)
	case "findOne":
		filter, err := getMongoDocument(args, 0, "filter")
		if err != nil {
			return err
		}
		opts := options.FindOne()
		if len(args) > 1 {
			projection, err := getMongoDocument(args, 1, "projection")
			if err != nil {
				return err
			}
			opts.SetProjection(projection)
		}
		raw, err := coll.FindOne(ctx, filter, opts).Raw()
		if errors.Is(err, mongo.ErrNoDocuments) {
			addMongoDocuments(res, nil)
			return nil
		}
		if err != nil {
			return err
		}
		addMongoDocuments(res, []bson.Raw{raw})
		res.RowCount = 1
		return nil
	case "countDocuments", "count":
		filter, err := getMongoDocument(args, 0, "filter")
		if err != nil {
			return err
		}
		n, err := coll.CountDocuments(ctx, filter)
		if err != nil {
			return err
		}
		addMongoValues(res, "count", []string{strconv.FormatInt(n, 10)})
		return nil
	case "estimatedDocumentCount":
		n, err := coll.EstimatedDocumentCount(ctx)
		if err != nil {
			return err
		}
		addMongoValues(res, "count", []string{strconv.FormatInt(n, 10)})
		return nil
	case "distinct":
		field, ok := firstArg(args).(string)
		if !ok || field == "" {
			return fmt.Errorf("distinct() takes a field name")
		}
		filter, err := getMongoDocument(args, 1, "filter")
		if err != nil {
			return err
		}
		array, err := coll.Distinct(ctx, field, filter).Raw()
		if err != nil {
			return err
		}
		values, err := array.Values()
		if err != nil {
			return err
		}
		res.Columns = []SqlColumn{{Name: field}}
		res.Rows = [][]*string{}
		for _, value := range values {
			res.RowCount++
			if len(res.Rows) >= SqlMaxRows {
				res.Truncated = true
				continue
			}
			res.Rows = append(res.Rows, []*string{formatMongoValue(value)})
		}
		return nil
	case "aggregate":
		pipeline, ok := firstArg(args).(bson.A)
		if !ok {
			return fmt.Errorf("aggregate() takes a pipeline such as [{$match: {}}]")
		}
		cursor, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return err
		}
		return readMongoCursor(ctx, cursor, res)
	case "insertOne":
		doc, err := getMongoDocument(args, 0, "document")
		if err != nil {
			return err
		}
		_, err = coll.InsertOne(ctx, doc)
		if err != nil {
			return err
		}
		res.RowCount = 1
		return nil
	case "insertMany":
		docs, ok := firstArg(args).(bson.A)
		if !ok || len(docs) == 0 {
			return fmt.Errorf("insertMany() takes an array of documents")
		}
		result, err := coll.InsertMany(ctx, docs)
		if result != nil {
			res.RowCount = int64(len(result.InsertedIDs))
		}
		return err
	case "updateOne", "updateMany", "replaceOne":
		if len(args) < 2 {
			return fmt.Errorf("%s() takes a filter and an update", call.method)
		}
		filter, err := getMongoDocument(args, 0, "filter")
		if err != nil {
			return err
		}
		upsert := false
		if len(args) > 2 {
			opts, err := getMongoDocument(args, 2, "options")
			if err != nil {
				return err
			}
			upsert = getMongoBool(opts, "upsert")
		}

		var result *mongo.UpdateResult
		switch call.method {
		case "updateOne":
			result, err = coll.UpdateOne(ctx, filter, args[1], options.UpdateOne().SetUpsert(upsert))
		case "updateMany":
			result, err = coll.UpdateMany(ctx, filter, args[1], options.UpdateMany().SetUpsert(upsert))
		default:
			result, err = coll.ReplaceOne(ctx, filter, args[1], options.Replace().SetUpsert(upsert))
		}
		if err != nil {
			return err
		}
		res.RowCount = result.ModifiedCount + result.UpsertedCount
		return nil
	case "deleteOne", "deleteMany":
		filter, err := getMongoDocument(args, 0, "filter")
		if err != nil {
			return err
		}
		if len(args) == 0 {
			return fmt.Errorf("%s() takes a filter, {} for all the documents", call.method)
		}
		var result *mongo.DeleteResult
		if call.method == "deleteOne" {
			result, err = coll.DeleteOne(ctx, filter)
		} else {
			result, err = coll.DeleteMany(ctx, filter)
		}
		if err != nil {
			return err
		}
		res.RowCount = result.DeletedCount
		return nil
	case "drop":
		return coll.Drop(ctx)
	case "createIndex":
		keys, err := getMongoDocument(args, 0, "index keys")
		if err != nil || len(keys) == 0 {
			return fmt.Errorf("createIndex() takes the keys such as {name: 1}")
		}
		model := mongo.IndexModel{Keys: keys}
		if len(args) > 1 {
			opts, err := getMongoDocument(args, 1, "options")
			if err != nil {
				return err
			}
			indexOptions := options.Index()
			for _, e := range opts {
				switch e.Key {
				case "name":
					if name, ok := e.Value.(string); ok {
						indexOptions.SetName(name)
					}
				case "unique":
					indexOptions.SetUnique(getMongoBool(opts, "unique"))
				case "sparse":
					indexOptions.SetSparse(getMongoBool(opts, "sparse"))
				case "expireAfterSeconds":
					if n, ok := toMongoInt(e.Value); ok {
						indexOptions.SetExpireAfterSeconds(int32(n))
					}
				default:
					return fmt.Errorf("the index option: %s is not supported", e.Key)
				}
			}
			model.Options = indexOptions
		}
		name, err := coll.Indexes().CreateOne(ctx, model)
		if err != nil {
			return err
		}
		addMongoValues(res, "name", []string{name})
		return nil
	case "dropIndex":
		switch v := firstArg(args).(type) {
		case string:
			return coll.Indexes().DropOne(ctx, v)
		case bson.D:
			return coll.Indexes().DropWithKey(ctx, v)
		}
		return fmt.Errorf("dropIndex() takes an index name or its keys")
	case "getIndexes":
		cursor, err := coll.Indexes().List(ctx)
		if err != nil {
			return err
		}
		return readMongoCursor(ctx, cursor, res)
	case "stats":
		return d.runCommand(coll.Database(), bson.D{{Key: "collStats", Value: coll.Name()}}, res)
	}
	return fmt.Errorf("the collection method: %s() is not supported", call.method)
}

func (d *mongodbDriver) find(coll *mongo.Collection, call *mongoCall, res *SqlResult) error {
	ctx := d.c.ctx
	filter, err := getMongoDocument(call.args, 0, "filter")
	if err != nil {
		return err
	}
	opts := options.Find()
	if len(call.args) > 1 {
		projection, err := getMongoDocument(call.args, 1, "projection")
		if err != nil {
			return err
		}
		opts.SetProjection(projection)
	}

	var limit, skip int64
	count := false
	for _, chained := range call.chain {
		switch chained.name {
		case "limit":
			limit, err = getMongoInt(chained.args, "limit")
		case "skip":
			skip, err = getMongoInt(chained.args, "skip")
			opts.SetSkip(skip)
		case "sort":
			var sort bson.D
			sort, err = getMongoDocument(chained.args, 0, "sort")
			opts.SetSort(sort)
		case "projection":
			var projection bson.D
			projection, err = getMongoDocument(chained.args, 0, "projection")
			opts.SetProjection(projection)
		case "hint":
			opts.SetHint(firstArg(chained.args))
		case "count", "itcount", "size":
			count = true
		case "pretty", "toArray", "batchSize":
		default:
			err = fmt.Errorf("%s() can't be chained after find()", chained.name)
		}
		if err != nil {
			return err
		}
	}

	if count {
		countOptions := options.Count()
		if skip > 0 {
			countOptions.SetSkip(skip)
		}
		if limit > 0 {
			countOptions.SetLimit(limit)
		}
		n, err := coll.CountDocuments(ctx, filter, countOptions)
		if err != nil {
			return err
		}
		addMongoValues(res, "count", []string{strconv.FormatInt(n, 10)})
		return nil
	}

	// without a limit, read one document more than is shown and count the rest on the server
	implicit := limit <= 0 || limit > SqlMaxRows
	if implicit {
		opts.SetLimit(SqlMaxRows + 1)
	} else {
		opts.SetLimit(limit)
	}
	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return err
	}
	err = readMongoCursor(ctx, cursor, res)
	if err != nil {
		return err
	}

	if implicit && res.Truncated {
		countCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		countOptions := options.Count()
		if skip > 0 {
			countOptions.SetSkip(skip)
		}
		if limit > 0 {
			countOptions.SetLimit(limit)
		}
		n, err := coll.CountDocuments(countCtx, filter, countOptions)
		if err == nil {
			res.RowCount = n
		}
	}
	return nil
}
