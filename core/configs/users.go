package configs

import "golang.org/x/crypto/bcrypt"

// NewUser is the function used to add a new user.
func NewUser(username, password string, admin bool) error {
	if usernameExists(username) {
		return ErrUsernameExists
	}

	hashedPassword, err := hashAndSalt([]byte(password))
	if err != nil {
		return err
	}

	newUser := User{
		username,
		hashedPassword,
		admin,
	}

	CurrentConfigs.Lock()
	CurrentConfigs.Users = append(CurrentConfigs.Users, newUser)
	CurrentConfigs.Unlock()
	err = WriteToFile()
	if err != nil {
		return err
	}

	return nil
}

// DeleteUser is used to delete a existing users.
func DeleteUser(username string) error {
	// TODO Verify if the user that executes this function (from the API)
	// is an admin user.
	CurrentConfigs.RLock()
	for index, user := range CurrentConfigs.Users {
		if user.Username == username {
			CurrentConfigs.RUnlock()
			CurrentConfigs.Lock()
			CurrentConfigs.Users = append(
				CurrentConfigs.Users[:index], CurrentConfigs.Users[index+1:]...,
			)
			CurrentConfigs.Unlock()
			err := WriteToFile()
			if err != nil {
				return err
			}

			return nil
		}
	}
	CurrentConfigs.RUnlock()
	return ErrUserNotFound
}

// AuthUser is used to authenticate a user.
func AuthUser(username, password string) (user User, authenticated bool) {
	CurrentConfigs.RLock()
	defer CurrentConfigs.RUnlock()
	for _, user := range CurrentConfigs.Users {
		if user.Username == username {
			err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
			if err != nil {
				return User{}, false
			}
			return user, true
		}
	}
	return User{}, false
}

// ChangeUserPassword is a function used to change the password of a user.
func ChangeUserPassword(username, newPassword string) error {
	CurrentConfigs.RLock()

	for _, user := range CurrentConfigs.Users {
		if user.Username == username {
			CurrentConfigs.RUnlock()
			hashedPwd, err := hashAndSalt([]byte(newPassword))
			if err != nil {
				return err
			}
			CurrentConfigs.Lock()
			user.PasswordHash = hashedPwd
			CurrentConfigs.Unlock()
			err = WriteToFile()
			if err != nil {
				return err
			}
			return nil
		}
	}

	CurrentConfigs.RUnlock()
	return ErrUserNotFound
}

func usernameExists(username string) bool {
	CurrentConfigs.RLock()
	defer CurrentConfigs.RUnlock()

	for _, user := range CurrentConfigs.Users {
		if user.Username == username {
			return true
		}
	}
	return false
}

func hashAndSalt(password []byte) (hashedPassword string, err error) {
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}
