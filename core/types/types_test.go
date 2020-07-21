package types

import (
	"fmt"
	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"math/rand"
	"net/url"
	"strconv"
	"testing"
)

type TypesTestSuite struct {
	TestCases map[PWType][]string

	suite.Suite
}

func (suite *TypesTestSuite) SetupTest() {
	suite.TestCases = map[PWType][]string{
		Int: {
			"3",
			"18",
			"157",
			"2904",
			"67894",
			"734573",
			"1221536",
			"81234897",
		},
		Float: {
			"2.5",
			"18.13",
			"157.431",
			"2904.4235",
			"67894.53634",
			"734573.785778",
			"1221536.4536763",
			"81234897.23462753",
		},
		Bool: {
			"true",
			"false",
			"TRUE",
			"FALSE",
			"True",
			"False",
		},
		Path: {
			".",
			"./../another_dir/file.deb",
			"./../another_dir",
			"./../../Images/img.png",
			"./../../Images",
			"../Downloads/my_file.txt",
			"../Downloads",
			"../../MyDir/file.zip",
			"../../MyDir",
			"/home/martin/Documents/doc.md",
			"/home/martin/Documents",
			"/home/martin/rustlings/src/main.rs",
			"/home/martin/rustlings/src",
			"/etc/systemd/system/custom.service",
			"/etc/systemd/system",
		},
		JSON: {
			"{\"glossary\":{\"title\":\"example glossary\",\"GlossDiv\":{\"title\":\"S\",\"GlossList\":{\"GlossEntry" +
				"\":{\"ID\":\"SGML\",\"SortAs\":\"SGML\",\"GlossTerm\":\"Standard Generalized Markup Language\",\"" +
				"Acronym\":\"SGML\",\"Abbrev\":\"ISO 8879:1986\",\"GlossDef\":{\"para\":\"A meta-markup language, " +
				"used to create markup languages such as DocBook.\",\"GlossSeeAlso\":[\"GML\",\"XML\"]},\"GlossSee\"" +
				":\"markup\"}}}}}",
			"[false,true,{\"final\":[{\"apart\":true,\"known\":\"all\",\"us\":\"chamber\"},[false,\"whom\",-1359658158" +
				".6242466],\"available\"],\"hot\":-1296640005,\"home\":\"fish\"}]",
			"{\"southern\":true,\"alphabet\":512830852,\"future\":true}",
			"[[-981014373.5451524,{\"brush\":false,\"needed\":{\"fish\":535049700,\"appearance\":\"talk\",\"famous\"" +
				":-1738204912.7989771},\"frame\":\"whose\"},true],\"process\",\"weigh\"]",
			"[\"mass\",[true,[1403645379.6560357,-181048498.2748382,782711318.1330621],false],{\"birds\":{\"out\"" +
				":false,\"raise\":{\"having\":true,\"escape\":\"medicine\",\"principle\":\"suppose\"},\"sell\":1722" +
				"65610.25941133},\"sun\":6957194.759199858,\"bare\":true}]",
		},
		URL: {
			"http://example.com/",
			"http://example.com/#ants",
			"http://www.example.org/",
			"https://bubble.example.net/",
			"http://www.example.net/?berry=birds",
			"http://www.example.net/activity",
			"https://duckduckgo.com/",
			"https://github.com",
			"https://github.com/Pegasus8/piworker",
			"https://github.com/Pegasus8/piworker/issues/89",
			"https://www.jetbrains.com/",
			"https://github.com/ohmyzsh/ohmyzsh/tree/master/plugins/golang",
		},
		Date: {
			"15-07-2020",
			"15/07/2020",
			"2020-07-15",
			"01-01-2021",
			"01/01/2021",
			"2021-01-01",
			"10-10-2021",
			"10/10/2021",
			"2021-10-10",
		},
		Time: {
			"12:12:56",
			"12:12",
			"09:24:34",
			"09:24",
			"13:09:10",
			"13:09",
			"16:23:01",
			"09:01",
			"23:59:59",
			"23:59",
		},
	}
}

func (suite *TypesTestSuite) BeforeTest(_, _ string) {}

func (suite *TypesTestSuite) TestIsInt() {
	assert := assert2.New(suite.T())

	for i, intStr := range suite.TestCases[Int] {
		isInt, intV := IsInt(intStr)

		value, err := strconv.ParseInt(intStr, 10, 64)
		if err != nil {
			panic(fmt.Errorf("[suite.TestCases[Int][%d]]: %s", i, err.Error()))
		}

		assert.Truef(isInt, "[suite.TestCases[Int][%d]]: the value should be recognized as an integer", i)
		assert.Equalf(value, intV, "[suite.TestCases[Int][%d]]: value not converted correctly", i)
	}

	for v := range suite.TestCases {
		if v == Int {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isInt, i := IsInt(suite.TestCases[v][r])
		assert.Falsef(isInt, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as an 'Int'", string(v), r, string(v))
		assert.Equalf(int64(0), i, "[suite.TestCases[%s][%d]]: if the value is not of type 'Int' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestIsFloat() {
	assert := assert2.New(suite.T())

	for i, floatStr := range suite.TestCases[Float] {
		isFloat, floatV := IsFloat(floatStr)

		value, err := strconv.ParseFloat(floatStr, 64)
		if err != nil {
			panic(fmt.Errorf("[suite.TestCases[Float][%d]]: %s", i, err.Error()))
		}

		assert.Truef(isFloat, "[suite.TestCases[Float][%d]]: the value should be recognized as a float", i)
		assert.Equalf(value, floatV, "[suite.TestCases[Float][%d]]: value not converted correctly", i)
	}

	for v := range suite.TestCases {
		// 'Int' is avoided too because it's automatically converted to float.
		if v == Float || v == Int {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isFloat, i := IsFloat(suite.TestCases[v][r])
		assert.Falsef(isFloat, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as a 'Float'", string(v), r, string(v))
		assert.Equalf(0.0, i, "[suite.TestCases[%s][%d]]: if the value is not of type 'Float' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestIsBool() {
	assert := assert2.New(suite.T())

	for i, boolStr := range suite.TestCases[Bool] {
		isBool, boolV := IsBool(boolStr)

		value, err := strconv.ParseBool(boolStr)
		if err != nil {
			panic(fmt.Errorf("[suite.TestCases[Bool][%d]]: %s", i, err.Error()))
		}

		assert.Truef(isBool, "[suite.TestCases[Bool][%d]]: the value should be recognized as a boolean", i)
		assert.Equalf(value, boolV, "[suite.TestCases[Bool][%d]]: value not converted correctly", i)
	}

	for v := range suite.TestCases {
		if v == Bool {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isBool, i := IsBool(suite.TestCases[v][r])
		assert.Falsef(isBool, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as a 'Bool'", string(v), r, string(v))
		assert.Equalf(false, i, "[suite.TestCases[%s][%d]]: if the value is not of type 'Bool' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestIsPath() {
	assert := assert2.New(suite.T())

	for i, pathStr := range suite.TestCases[Path] {
		isPath, _ := IsPath(pathStr)

		assert.Truef(isPath, "[suite.TestCases[Path][%d]]: the value '%s' should be recognized as a path", i, pathStr)
	}

	// There is a bit complicated situation: a lot of values can be considered as a valid path.
	// For example, if the input is "1" (type `Int`), there can be a directory called "1" or a file called with the same name.
	// The same happens with other types like `Time`, `Float`, `Bool`, etc. That won't be a problem, but to make sure
	// that a type is not confused with another, the function `GetType` should implement `IsPath` at the end. On other
	// side, maybe you are wondering "what if the user want to use a number as the name of a directory?" well, that's
	// why the method `PWType.CompatWith` exists.
}

func (suite *TypesTestSuite) TestIsJSON() {
	assert := assert2.New(suite.T())

	for i, jsonStr := range suite.TestCases[JSON] {
		isJSON := IsJSON(jsonStr)

		assert.Truef(isJSON, "[suite.TestCases[JSON][%d]]: the value '%s' should be recognized as json", i, jsonStr)
	}

	for v := range suite.TestCases {
		// All those values will be parsed correctly as JSON, something that we don't want here.
		if v == JSON || v == Int || v == Float {
			continue
		}

		// Avoid the values "true" (0) or "false" (1) which are considered as valid JSON objects.
		var r int
		for {
			r = rand.Intn(len(suite.TestCases[v]))
			if !(v == Bool && r < 2) {
				break
			}
		}

		// Should return false.
		isJSON := IsJSON(suite.TestCases[v][r])
		assert.Falsef(isJSON, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as 'JSON'", string(v), r, string(v))
	}
}

func (suite *TypesTestSuite) TestIsURL() {
	assert := assert2.New(suite.T())

	for i, urlStr := range suite.TestCases[URL] {
		isURL, urlV := IsURL(urlStr)

		value, err := url.Parse(urlStr)
		if err != nil {
			panic(fmt.Errorf("[suite.TestCases[URL][%d]]: %s", i, err.Error()))
		}

		assert.Truef(isURL, "[suite.TestCases[URL][%d]]: the value should be recognized as a url", i)
		assert.Equalf(value, urlV, "[suite.TestCases[URL][%d]]: value not converted correctly", i)
	}

	for v := range suite.TestCases {
		if v == URL {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isURL, i := IsURL(suite.TestCases[v][r])
		assert.Falsef(isURL, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as a 'URL'", string(v), r, string(v))
		assert.Nilf(i, "[suite.TestCases[%s][%d]]: if the value is not of type 'URL' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestIsDate() {
	assert := assert2.New(suite.T())

	for i, dateStr := range suite.TestCases[Date] {
		isDate, _ := IsDate(dateStr)

		assert.Truef(isDate, "[suite.TestCases[Date][%d]]: the value should be recognized as a date", i)
	}

	for v := range suite.TestCases {
		if v == Date {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isDate, i := IsDate(suite.TestCases[v][r])
		assert.Falsef(isDate, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as a 'Date'", string(v), r, string(v))
		assert.Truef(i.IsZero(), "[suite.TestCases[%s][%d]]: if the value is not of type 'Date' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestIsTime() {
	assert := assert2.New(suite.T())

	for i, timeStr := range suite.TestCases[Time] {
		isTime, _ := IsTime(timeStr)

		assert.Truef(isTime, "[suite.TestCases[Time][%d]]:the value should be recognized as a time", i)
	}

	for v := range suite.TestCases {
		if v == Time {
			continue
		}

		r := rand.Intn(len(suite.TestCases[v]))

		// Should return false and no convert the value.
		isTime, i := IsTime(suite.TestCases[v][r])
		assert.Falsef(isTime, "[suite.TestCases[%s][%d]]: the value of type '%s' should not be considered as a 'Time'", string(v), r, string(v))
		assert.Truef(i.IsZero(), "[suite.TestCases[%s][%d]]: if the value is not of type 'Time' the conversion should not be executed", string(v), r)
	}
}

func (suite *TypesTestSuite) TestGetType() {
	assert := assert2.New(suite.T())

	for t, s := range suite.TestCases {
		for i, st := range s {
			returnedType := GetType(st)
			assert.Equalf(t, returnedType, "[%d] the returned type is not which is supposed to be", i)
		}
	}
}

func (suite *TypesTestSuite) TearDownTest() {}

func TestTypesSuite(t *testing.T) {
	suite.Run(t, new(TypesTestSuite))
}
