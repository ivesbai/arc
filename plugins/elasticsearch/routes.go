package elasticsearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/appbaseio-confidential/arc/arc/route"
	"github.com/appbaseio-confidential/arc/internal/types/acl"
	"github.com/appbaseio-confidential/arc/internal/types/category"
	"github.com/appbaseio-confidential/arc/internal/types/op"
	"github.com/appbaseio-confidential/arc/internal/util"
)

var (
	routes     []route.Route
	routeSpecs = make(map[string]api)
	acls       = make(map[category.Category]map[acl.ACL]bool)
)

type api struct {
	name     string
	category category.Category
	acl      acl.ACL
	op       op.Operation
	spec     *spec
}

type spec struct {
	Documentation string   `json:"documentation"`
	Methods       []string `json:"methods"`
	URL           struct {
		Path   string      `json:"path"`
		Paths  []string    `json:"paths,omitempty"`
		Parts  interface{} `json:"parts,omitempty"`
		Params interface{} `json:"params,omitempty"`
	} `json:"url"`
	Body struct {
		Description string `json:"description"`
		Required    bool   `json:"required,omitempty"`
		Serialize   string `json:"serialize,omitempty"`
	} `json:"body,omitempty"`
}

func (es *elasticsearch) preprocess() error {
	files := make(chan string)
	apis := make(chan api)

	path, err := getWD()
	if err != nil {
		return fmt.Errorf("unable to get the working directory: %v", err)
	}

	go fetchSpecFiles(path, files)
	go decodeSpecFiles(files, apis)

	middleware := (&chain{}).Wrap

	for api := range apis {
		for _, path := range api.spec.URL.Paths {
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if path == "/" {
				continue
			}
			r := route.Route{
				Name:        api.name,
				Methods:     api.spec.Methods,
				Path:        path,
				HandlerFunc: middleware(es.handler()),
				Description: api.spec.Documentation,
			}
			routes = append(routes, r)
			for _, method := range api.spec.Methods {
				key := fmt.Sprintf("%s:%s", method, path)
				routeSpecs[key] = api
			}
		}
		if _, ok := acls[api.category]; !ok {
			acls[api.category] = make(map[acl.ACL]bool)
		}
		if _, ok := acls[api.category][api.acl]; !ok {
			acls[api.category][api.acl] = true
		}
	}

	// sort the routes
	criteria := func(r1, r2 route.Route) bool {
		f1, c1 := util.CountComponents(r1.Path)
		f2, c2 := util.CountComponents(r2.Path)
		if f1 == f2 {
			return c1 < c2
		}
		return f1 > f2
	}
	route.By(criteria).Sort(routes)

	// append index route last in order to avoid early matches for other specific routes
	indexRoute := route.Route{
		Name:        "ping",
		Methods:     []string{http.MethodGet},
		Path:        "/",
		HandlerFunc: middleware(es.handler()),
		Description: "You know, for search",
	}
	routes = append(routes, indexRoute)

	return nil
}

func (es *elasticsearch) routes() []route.Route {
	return routes
}

func getWD() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", nil
	}
	return filepath.Join(wd, "plugins/elasticsearch/api"), nil
}

func fetchSpecFiles(path string, files chan<- string) {
	defer close(files)

	info, err := os.Stat(path)
	if err != nil {
		log.Fatal(err)
		return
	}
	if !info.IsDir() {
		log.Printf("cannot walk through a file %s", path)
		return
	}

	err = filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && filepath.Ext(path) == ".json" && !strings.HasPrefix(info.Name(), "_") {
			files <- path
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
		return
	}
}

func decodeSpecFiles(files <-chan string, apis chan<- api) {
	var wg sync.WaitGroup
	for file := range files {
		wg.Add(1)
		go decodeSpecFile(file, &wg, apis)
	}

	go func() {
		wg.Wait()
		close(apis)
	}()
}

func decodeSpecFile(file string, wg *sync.WaitGroup, apis chan<- api) {
	defer wg.Done()

	content, err := ioutil.ReadFile(file)
	if err != nil {
		log.Printf("can't read file: %v", err)
		return
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	_, err = decoder.Token() // skip opening braces
	if err != nil {
		log.Fatal(err)
		return
	}
	_, err = decoder.Token() // skip object name
	if err != nil {
		log.Fatal(err)
		return
	}

	var s spec
	err = decoder.Decode(&s)
	if err != nil {
		log.Fatal(err)
		return
	}

	specName := strings.TrimSuffix(filepath.Base(file), ".json")
	specCategory := decodeCategory(&s)
	specOp := decodeOp(&s)
	specACL, err := decodeACL(specName, &s)
	if err != nil {
		log.Printf(`%s: unable to categorize spec "%s": %v\n`, logTag, specName, err)
	}

	apis <- api{
		name:     specName,
		category: specCategory,
		op:       specOp,
		acl:      *specACL,
		spec:     &s,
	}
}

func decodeCategory(spec *spec) category.Category {
	docTokens := strings.Split(spec.Documentation, "/")
	tag := strings.TrimSuffix(docTokens[len(docTokens)-1], ".html")
	tagTokens := strings.Split(tag, "-")
	tagName := tagTokens[0]
	return category.FromString(tagName)
}

func decodeACL(specName string, spec *spec) (*acl.ACL, error) {
	pathTokens := strings.Split(spec.URL.Path, "/")
	for _, pathToken := range pathTokens {
		if strings.HasPrefix(pathToken, "_") {
			pathToken = strings.TrimPrefix(pathToken, "_")
			c, err := acl.FromString(pathToken)
			if err != nil {
				return nil, err
			}
			return &c, nil
		}
	}

	aclString := strings.Split(specName, ".")[0]
	a, err := acl.FromString(aclString)
	if err != nil {
		defaultACL := acl.Get
		return &defaultACL, err
	}

	return &a, nil
}

func decodeOp(spec *spec) op.Operation {
	var specOp op.Operation
	methods := spec.Methods

out:
	for _, method := range methods {
		switch method {
		case http.MethodPut:
			specOp = op.Write
			break out
		case http.MethodPatch:
			specOp = op.Write
			break out
		case http.MethodDelete:
			specOp = op.Delete
			break out
		case http.MethodGet:
			specOp = op.Read
			break out
		case http.MethodHead:
			specOp = op.Read
			break out
		case http.MethodPost:
			specOp = op.Write
		default:
			specOp = op.Read
			break out
		}
	}

	return specOp
}

func printCategoryACLMDTable() {
	fmt.Printf("| **Category** | **ACLs** |\n")
	fmt.Printf("|----------|------|\n")
	for c, a := range acls {
		fmt.Printf("| `%s` | ", c)
		fmt.Printf("<ul>")
		for k := range a {
			fmt.Printf("<li>`%s`</li>", k)
		}
		fmt.Printf("</ul> |")
		fmt.Printf("\n")
	}
}
