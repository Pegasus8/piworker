package configs

import (
	"os"
	"testing"

	assert2 "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"golang.org/x/crypto/bcrypt"
)

type customUser struct {
	Username string
	Password string
	Admin    bool
}

type UMTestSuite struct {
	TestDir   string
	TestUsers []customUser

	suite.Suite
}

func (suite *UMTestSuite) SetupTest() {
	suite.TestDir = "./test"
	Path = suite.TestDir

	err := os.Mkdir(suite.TestDir, 0755)
	if err != nil {
		panic(err)
	}

	CurrentConfigs = &Configs{}

	suite.TestUsers = []customUser{
		{
			Username: "User 0",
			Password: "hello melon banana potato",
			Admin:    true,
		},
		{
			Username: "User 1",
			Password: "hello melon banana potato 1234",
			Admin:    true,
		},
		{
			Username: "User 2",
			Password: "Hello world! ¿¡?'ª/",
			Admin:    true,
		},
		{
			Username: "User 3",
			Password: "How are you? -_:;,.",
			Admin:    false,
		},
		{
			Username: "User 4",
			Password: "I like numbers 1234567890",
			Admin:    true,
		},
	}
}

func (suite *UMTestSuite) BeforeTest(_, _ string) {
	for _, user := range suite.TestUsers[:2] {
		err := NewUser(user.Username, user.Password, user.Admin)
		if err != nil {
			panic(err)
		}
	}
}

func (suite *UMTestSuite) TestNewUser() {
	assert := assert2.New(suite.T())

	// Add each user and check if they are correctly saved in the variable `CurrentConfigs.Users` and in the config file.
	for i, user := range suite.TestUsers[2:] {
		err := NewUser(user.Username, user.Password, user.Admin)
		assert.NoError(err, "The user should be created without errors")

		// Check if the user is in the variable.
		CurrentConfigs.RLock()
		if len(CurrentConfigs.Users) != i+3 {
			CurrentConfigs.RUnlock()
			assert.FailNowf("User no added to the variable", "The new user (%+v) must be added to the "+
				"slice of users, in the variable `CurrentConfigs.Users`", user)
		}
		CurrentConfigs.RUnlock()

		inSlice := func() bool {
			CurrentConfigs.RLock()
			defer CurrentConfigs.RUnlock()

			for _, u := range CurrentConfigs.Users {
				if u.Username == user.Username {
					err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(user.Password))
					return err == nil
				}
			}

			return false
		}()
		assert.Equalf(true, inSlice, "The user %+v must be in the slice of users", user)

		// Update the variable from the config file to make sure that our user is there.
		err = ReadFromFile()
		if err != nil {
			panic(err)
		}
		CurrentConfigs.RLock()
		if len(CurrentConfigs.Users) != i+3 {
			CurrentConfigs.RUnlock()
			assert.FailNowf("User no added to the config file", "The new user (%+v) must be added to the "+
				"slice of users, in the config file", user)
		}
		CurrentConfigs.RUnlock()
		_, auth2 := AuthUser(user.Username, user.Password)
		assert.Equalf(true, auth2, "The user %+v must"+
			" be in the config file", user)
	}

	// Try to add a user with already taken username.
	err := NewUser(suite.TestUsers[2].Username, suite.TestUsers[2].Password, suite.TestUsers[2].Admin)
	assert.Error(err, "The user should not be able to use an already taken username")
	assert.EqualError(err, ErrUsernameExists.Error(), "The returned error on a case where the user tried"+
		" to use an already taken username must be `ErrUsernameExists`")
}

func (suite *UMTestSuite) TestAuthUser() {
	assert := assert2.New(suite.T())

	// Should be correctly authenticated.
	_, auth := AuthUser(suite.TestUsers[0].Username, suite.TestUsers[0].Password)
	assert.Equal(true, auth, "The user must be authenticated correctly")

	// The authentication must fail because the username does not exist.
	_, auth = AuthUser(suite.TestUsers[0].Username+"1 2 3 4", suite.TestUsers[0].Password)
	assert.Equal(false, auth, "The authentication should fail because the username does not exist")

	// The authentication must fail because the password is incorrect.
	_, auth = AuthUser(suite.TestUsers[0].Username, suite.TestUsers[0].Password+"eight")
	assert.Equal(false, auth, "The authentication should fail because the password is incorrect")

	// The authentication must fail because both, username and password, are incorrect.
	_, auth = AuthUser(suite.TestUsers[0].Username+"hello", suite.TestUsers[0].Password+"world")
	assert.Equal(false, auth, "The authentication should fail because the username and password"+
		" are incorrect")
}

func (suite *UMTestSuite) TestChangeUserPassword() {
	assert := assert2.New(suite.T())
	newPassword := "a super complicated password"

	// Change the password of the user without problems.
	err := ChangeUserPassword(suite.TestUsers[0].Username, newPassword)
	assert.NoError(err, "The password should be changed without problems")

	// The change of the password must fail because the username does not exist.
	err = ChangeUserPassword(suite.TestUsers[0].Username+"Johnson", newPassword)
	assert.Error(err, "The password shouldn't be changed because the username does not exist")
	assert.EqualError(err, ErrUserNotFound.Error(), "The returned error should be `ErrUserNotFound`")
}

func (suite *UMTestSuite) TestDeleteUser() {
	assert := assert2.New(suite.T())

	// The user must be deleted correctly.
	err := DeleteUser(suite.TestUsers[1].Username)
	assert.NoError(err, "The user must be deleted correctly")

	// The deletion should fail because the username does not exist.
	err = DeleteUser(suite.TestUsers[1].Username + " Random words")
	assert.Error(err, "The function should return an error because the username used does not exist")
	assert.EqualError(err, ErrUserNotFound.Error(), "The returned error should be `ErrUserNotFound`")
}

func (suite *UMTestSuite) TestUsernameExists() {
	assert := assert2.New(suite.T())

	// Username exists.
	r := usernameExists(suite.TestUsers[0].Username)
	assert.Equal(true, r, "The function should return true due that the username exists in"+
		" the slice of users")

	// Username does not exist.
	r = usernameExists(suite.TestUsers[0].Username + "John")
	assert.Equal(false, r, "The function should return false due that the username does not"+
		" exist in the slice of users")
}

func (suite *UMTestSuite) TearDownTest() {
	err := os.RemoveAll(suite.TestDir)
	if err != nil {
		panic(err)
	}
}

func TestUMSuite(t *testing.T) {
	suite.Run(t, new(UMTestSuite))
}
