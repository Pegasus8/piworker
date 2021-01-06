package configs

import "golang.org/x/crypto/bcrypt"

// NewUser is the function used to add a new user.
func (c *Configs) NewUser(username, password string, admin bool) error {
	if c.usernameExists(username) {
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

	c.Lock()
	defer c.Unlock()

	c.Users = append(c.Users, newUser)

	return c.unsafeSync()
}

// DeleteUser is used to delete a existing users.
func (c *Configs) DeleteUser(username string) error {
	// TODO Verify if the user that executes this function (from the API) is an admin user.
	c.Lock()
	defer c.Unlock()

	for index, user := range c.Users {
		if user.Username == username {
			c.Users = append(
				c.Users[:index], c.Users[index+1:]...,
			)

			return c.unsafeSync()
		}
	}

	return ErrUserNotFound
}

// AuthUser is used to authenticate a user.
func (c *Configs) AuthUser(username, password string) (user User, authenticated bool) {
	c.RLock()
	defer c.RUnlock()

	for _, user := range c.Users {
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
func (c *Configs) ChangeUserPassword(username, newPassword string) error {
	c.Lock()
	defer c.Unlock()

	for _, user := range c.Users {
		if user.Username == username {
			hashedPwd, err := hashAndSalt([]byte(newPassword))
			if err != nil {
				return err
			}

			user.PasswordHash = hashedPwd

			return c.unsafeSync()
		}
	}

	return ErrUserNotFound
}

func (c *Configs) usernameExists(username string) bool {
	c.RLock()
	defer c.RUnlock()

	for _, user := range c.Users {
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
