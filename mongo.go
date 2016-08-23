package yiigo

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type MongoBase struct {
	CollectionName string
}

type Sequence struct {
	Id  string `bson:"_id"`
	Seq int64  `bson:"seq"`
}

var (
	mongoSession *mgo.Session
	mongoMux     sync.Mutex
)

func initMongo() error {
	mongoMux.Lock()
	defer mongoMux.Unlock()

	if mongoSession == nil {
		var err error
		host := GetConfigString("mongo", "host", "localhost")
		port := GetConfigInt("mongo", "port", 27017)
		poolLimit := GetConfigInt("mongo", "poolLimit", 10)

		connStr := fmt.Sprintf("mongodb://%s:%d", host, port)

		mongoSession, err = mgo.Dial(connStr)

		if err != nil {
			LogError("connect to mongo server error: ", err.Error())
			return err
		}

		mongoSession.SetPoolLimit(poolLimit)
	}

	return nil
}

func getSession() (*mgo.Session, string, error) {
	if mongoSession == nil {
		err := initMongo()

		if err != nil {
			return nil, "", err
		}
	}

	session := mongoSession.Clone()
	db := GetConfigString("mongo", "database", "test")

	return session, db, nil
}

func (m *MongoBase) refreshSequence() (int64, error) {
	session, db, err := getSession()

	if err != nil {
		return 0, err
	}

	defer session.Close()

	c := session.DB(db).C("sequence")

	condition := bson.M{"_id": m.CollectionName}

	change := mgo.Change{
		Update:    bson.M{"$inc": bson.M{"seq": 1}},
		Upsert:    true,
		ReturnNew: true,
	}

	sequence := Sequence{}

	_, applyErr := c.Find(condition).Apply(change, &sequence)

	if applyErr != nil {
		LogError("mongo refresh sequence error: ", applyErr.Error())
		return 0, applyErr
	}

	return sequence.Seq, nil
}

/**
 * Insert ������¼
 * data �������� interface{} (ָ��)
 */
func (m *MongoBase) Insert(data interface{}) error {
	session, db, err := getSession()

	if err != nil {
		return err
	}

	defer session.Close()

	refVal := reflect.ValueOf(data)
	elem := refVal.Elem()

	if elem.Kind() != reflect.Struct {
		LogErrorf("cannot use (type %v) as mongo Insert param", elem.Type())
		return errors.New(fmt.Sprintf("cannot use (type %v) as mongo Insert param", elem.Type()))
	}

	id, seqErr := m.refreshSequence()

	if seqErr != nil {
		return seqErr
	}

	elem.Field(0).SetInt(id)

	c := session.DB(db).C(m.CollectionName)

	insertErr := c.Insert(data)

	if insertErr != nil {
		LogErrorf("mongo collection %s insert error: %s", m.CollectionName, insertErr.Error())
		return insertErr
	}

	return nil
}

/**
 * Update ���¼�¼
 * query ��ѯ���� bson.M (map[string]interface{})
 * data �����ֶ� bson.M (map[string]interface{})
 */
func (m *MongoBase) Update(query bson.M, data bson.M) error {
	session, db, err := getSession()

	if err != nil {
		return err
	}

	defer session.Close()

	c := session.DB(db).C(m.CollectionName)

	updateErr := c.Update(query, bson.M{"$set": data})

	if updateErr != nil {
		LogErrorf("mongo collection %s update error: %s", m.CollectionName, updateErr.Error())
		return updateErr
	}

	return nil
}

/**
 * Increment ����
 * query ��ѯ���� bson.M (map[string]interface{})
 * column �����ֶ� string
 * inc ���� int
 */
func (m *MongoBase) Increment(query bson.M, column string, incr int) error {
	session, db, err := getSession()

	if err != nil {
		return err
	}

	defer session.Close()

	c := session.DB(db).C(m.CollectionName)

	data := bson.M{column: incr}
	updateErr := c.Update(query, bson.M{"$inc": data})

	if updateErr != nil {
		LogErrorf("mongo collection %s update error: %s", m.CollectionName, updateErr.Error())
		return updateErr
	}

	return nil
}

/**
 * FindOne ��ѯ
 * data ��ѯ���� interface{} (ָ��)
 * query ��ѯ���� bson.M (map[string]interface{})
 */
func (m *MongoBase) FindOne(data interface{}, query bson.M) error {
	session, db, err := getSession()

	if err != nil {
		return err
	}

	defer session.Close()

	c := session.DB(db).C(m.CollectionName)

	findErr := c.Find(query).One(data)

	if findErr != nil {
		LogErrorf("mongo collection %s findone error: %s", m.CollectionName, findErr.Error())
		return findErr
	}

	return nil
}

/**
 * Find ��ѯ
 * data ��ѯ���� interface{} (��Ƭָ�룺*[]struct{})
 * query ��ѯ���� map[string]interface{}
 * options map[string]interface{}
 * [
 *      count *int
 *      order string
 *      skip int
 *      limit int
 * ]
 */
func (m *MongoBase) Find(data interface{}, query bson.M, options ...map[string]interface{}) error {
	session, db, err := getSession()

	if err != nil {
		return err
	}

	defer session.Close()

	q := session.DB(db).C(m.CollectionName).Find(query)

	if count, ok := options[0]["count"]; ok {
		refVal := reflect.ValueOf(count)
		elem := refVal.Elem()

		total, countErr := q.Count()

		if countErr != nil {
			LogError("mongo collection %s count error: %s", m.CollectionName, countErr.Error())
			elem.Set(reflect.ValueOf(0))
		} else {
			elem.Set(reflect.ValueOf(total))
		}
	}

	if order, ok := options[0]["order"]; ok {
		if ordStr, ok := order.(string); ok {
			ordArr := strings.Split(ordStr, ",")
			q = q.Sort(ordArr...)
		}
	}

	if skip, ok := options[0]["skip"]; ok {
		if skp, ok := skip.(int); ok {
			q = q.Skip(skp)
		}
	}

	if limit, ok := options[0]["limit"]; ok {
		if lmt, ok := limit.(int); ok {
			q = q.Limit(lmt)
		}
	}

	findErr := q.All(data)

	if findErr != nil {
		LogErrorf("mongo collection %s find error: %s", m.CollectionName, findErr.Error())
		return findErr
	}

	return nil
}
