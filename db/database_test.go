package db

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"testing"
)

// ensure that database is initialized
func TestInit(t *testing.T) {
	pb := testbase(false)
	defer pb.Close()
	pb.Init()

	if !pb.IsInitialized() {
		t.Error("TestInit: database is not initialized")
	}
}

func TestGetStats(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()

	stats := pb.GetStats()
	if stats.Packages != 0 {
		t.Errorf("TestGetStats: unexpected package count %d", stats.Packages)
	}
	if stats.Marks != 0 {
		t.Errorf("TestGetStats: unexpected mark count %d", stats.Marks)
	}
	if stats.Documents != 0 {
		t.Errorf("TestGetStats: unexpected document count %d", stats.Documents)
	}
	if stats.Files != 0 {
		t.Errorf("TestGetStats: unexpected file count %d", stats.Files)
	}
}

func TestSetSequence(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()

	expected := 100

	pb.SetSequence(expected)
	actual := pb.GetSequence()

	if actual != expected {
		t.Errorf("TestSetSequence: unexpected sequence number %d", actual)
	}
}

func TestGetSetCache(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()
	key := "key"
	value := "value"

	// set cache
	pb.setCache(key, value)

	// get cache decoder
	decoder := pb.getCacheDecoder(key)
	var actual string
	err := decoder.Decode(&actual)
	if err != nil {
		t.Errorf("TestGetSetCache: unexpected error %v", err)
	}

	if actual != value {
		t.Errorf("TestGetSetCache: unexpected value %s", actual)
	}
}

func TestPutIncompletePackages(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()
	allDocs := []*BarePackage{
		{
			ID:       "Test",
			Revision: "Revision",
		},
		{
			ID:       "Test2",
			Revision: "Revision",
		},
	}

	pb.PutPackages(allDocs)

	actual := pb.GetIncompletePackages()
	if !reflect.DeepEqual(actual, allDocs) {
		t.Errorf("TestPutIncompletePackages: expected %q actual %q", allDocs, actual)
	}
}

func TestDeletePackage(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()
	allDocs := []*BarePackage{
		{
			ID:       "Test",
			Revision: "Revision",
		},
		{
			ID:       "Test2",
			Revision: "Revision",
		},
	}

	pb.PutPackages(allDocs)
	pb.DeletePackage("Test2")

	actual := pb.GetRevision("Test2")
	if actual != "" {
		t.Error("TestDeletePackage: unexpected behavior")
	}
}

func TestPutCompletedPackage(t *testing.T) {
	pb := testbase(true)
	defer pb.Close()

	allDocs := []*BarePackage{
		{
			ID:       "Test",
			Revision: "Revision",
		},
		{
			ID:       "Test2",
			Revision: "Revision",
		},
	}

	pb.PutPackages(allDocs)

	doc := `{"_id":"Test","rev":"Revision"}`
	files := []*url.URL{
		{
			Scheme: "http",
			Host:   "localhost",
			Path:   "test/-/test-0.0.1.tgz",
		},
	}
	pb.PutCompleted(allDocs[0], doc, "Revision", files)

	count := pb.GetCountOfMarks(true)
	if count != 1 {
		t.Errorf("TestPutCompletedPackage: expected count 1 actual %d", count)
	}

	actualDoc, actualFiles, err := pb.GetDocument("Test", true)
	if err != nil {
		t.Errorf("TestPutCompletedPackage: unexpected error %v", err)
	}
	if actualDoc != doc {
		t.Errorf("TestPutCompletedPackage: expected doc %s actual %s", doc, actualDoc)
	}
	if fmt.Sprintf("%v", actualFiles) != fmt.Sprintf("%v", files) {
		t.Errorf("TestPutCompletedPackage: expected files %q actual %q", files, actualFiles)
	}
}

func testbase(init bool) *PocketBase {
	conf := config()
	pb := NewPocketBase(conf, false)
	if init {
		pb.Init()
	}
	return pb
}

// config returns a config set for tests
func config() *DatabaseConfig {
	temp := tempfile()
	return &DatabaseConfig{
		Path:          temp,
		MaxCacheSize:  1024,
		CacheLifetime: 60,
	}
}

// tempfile returns a temporary file path.
func tempfile() string {
	f, err := ioutil.TempFile("", "bolt-")
	if err != nil {
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
	if err := os.Remove(f.Name()); err != nil {
		panic(err)
	}
	return f.Name()
}
