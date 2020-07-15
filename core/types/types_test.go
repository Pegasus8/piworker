package types

import (
	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
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
			"tRUE",
			"fALSE",
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
			"15-7-2020",
			"15/7/2020",
			"2020-7-15",
			"15-07-2020",
			"15/07/2020",
			"2020-07-15",
			"1-1-2021",
			"1/1/2021",
			"2021-1-1",
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
			"9:24:34",
			"09:24:34",
			"9:24",
			"09:24",
			"13:9:10",
			"13:09:10",
			"13:9",
			"13:09",
			"16:23:1",
			"16:23:01",
			"9:1",
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
			panic("[suite.TestCases[Int][" + string(i) + "]: " + err.Error())
		}

		assert.True(isInt, "the value should be recognized as an integer")
		assert.Equal(value, intV, "value not converted correctly")
	}

	for v := range suite.TestCases {
		if v == Int {
			continue
		}

		// Should return false and no convert the value.
		isInt, i := IsInt(suite.TestCases[v][0])
		assert.Falsef(isInt, "the value of type '%s' should not be considered as an 'Int'", string(v))
		assert.Equal(int64(0), i, "if the value is not of type 'Int' the conversion should not be executed")
	}
}

func (suite *TypesTestSuite) TestIsFloat() {
	assert := assert2.New(suite.T())

	for i, floatStr := range suite.TestCases[Float] {
		isFloat, floatV := IsFloat(floatStr)

		value, err := strconv.ParseFloat(floatStr, 64)
		if err != nil {
			panic("[suite.TestCases[Float][" + string(i) + "]: " + err.Error())
		}

		assert.True(isFloat, "the value should be recognized as a float")
		assert.Equal(value, floatV, "value not converted correctly")
	}

	for v := range suite.TestCases {
		// 'Int' is avoided too because it's automatically converted to float.
		if v == Float || v == Int {
			continue
		}

		// Should return false and no convert the value.
		isFloat, i := IsFloat(suite.TestCases[v][0])
		assert.Falsef(isFloat, "the value of type '%s' should not be considered as a 'Float'", string(v))
		assert.Equal(0.0, i, "if the value is not of type 'Float' the conversion should not be executed")
	}
}

func (suite *TypesTestSuite) TestIsBool() {

}

func (suite *TypesTestSuite) TestIsPath() {

}

func (suite *TypesTestSuite) TestIsJSON() {

}

func (suite *TypesTestSuite) TestIsURL() {

}

func (suite *TypesTestSuite) TestIsDate() {

}

func (suite *TypesTestSuite) TestIsTime() {

}

func (suite *TypesTestSuite) TestGetType() {

}

func (suite *TypesTestSuite) TearDownTest() {}

func TestTypesSuite(t *testing.T) {
	suite.Run(t, new(TypesTestSuite))
}
