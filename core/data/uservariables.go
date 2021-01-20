package data

import (
	"database/sql"
	"fmt"
	"github.com/Pegasus8/piworker/core/uservariables"
	"github.com/rs/zerolog/log"
)

// SetULV sets the content of a user local variable stored in the database. If the variable doesn't exist, it will be
// created with the given content.
func (db *DatabaseInstance) SetULV(v *uservariables.LocalVariable) error {
	exists, err := ulvExists(db, v.Name)
	if err != nil {
		return err
	}

	r, err := func() (sql.Result, error) {
		var q string
		if exists {
			q = "UPDATE variables_local SET name = ?, content = ?, type = ?, parent_task_id = ? WHERE name = ?;"
			return db.instance.Exec(q, v.Name, v.Content, v.Type, v.ParentTaskID, v.Name)
		} else {
			q = `INSERT INTO variables_local(name, content, type, parent_task_id) values (?, ?, ?, ?);`
			return db.instance.Exec(q, v.Name, v.Content, v.Type, v.ParentTaskID)
		}
	}()

	if err != nil {
		return err
	}

	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("the variable with name '%s' was not inserted in the database", v.Name)
	}

	return nil
}

// GetULV returns the content of a user local variable stored in the database. If the requested variable doesn't exist,
// an error will be returned.
func (db *DatabaseInstance) GetULV(name string) (*uservariables.LocalVariable, error) {
	q := "SELECT * FROM variables_local WHERE name = ?;"

	row, err := db.instance.Query(q, name)
	if err != nil {
		return nil, err
	}

	defer func() {
		err = row.Close()
		if err != nil {
			log.Err(err).Msg("error when trying to close rows")
		}
	}()

	if !row.Next() {
		return nil, fmt.Errorf("there's no variable with the name '%s'", name)
	}

	var v uservariables.LocalVariable

	err = row.Scan(
		&v.ID,
		&v.Name,
		&v.Content,
		&v.Type,
		&v.ParentTaskID,
	)
	if err != nil {
		return nil, err
	}

	return &v, nil
}

// SetUGV sets the content of a user global variable stored in the database. If the variable doesn't exist, it will be
// created with the given content.
func (db *DatabaseInstance) SetUGV(v *uservariables.GlobalVariable) error {
	exists, err := ugvExists(db, v.Name)
	if err != nil {
		return err
	}

	r, err := func() (sql.Result, error) {
		var q string
		if exists {
			q = "UPDATE variables_global SET name = ?, content = ?, type = ? WHERE name = ?;"
			return db.instance.Exec(q, v.Name, v.Content, v.Type, v.Name)
		} else {
			q = `INSERT INTO variables_global(name, content, type) values (?, ?, ?);`
			return db.instance.Exec(q, v.Name, v.Content, v.Type)
		}
	}()

	if err != nil {
		return err
	}

	rowsAffected, err := r.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("the variable with name '%s' was not inserted in the database", v.Name)
	}

	return nil
}

// GetUGV returns the content of a user global variable stored in the database. If the requested variable doesn't exist,
// an error will be returned.
func (db *DatabaseInstance) GetUGV(name string) (*uservariables.GlobalVariable, error) {
	q := "SELECT * FROM variables_global WHERE name = ?;"

	row, err := db.instance.Query(q, name)
	if err != nil {
		return nil, err
	}

	defer func() {
		err = row.Close()
		if err != nil {
			log.Err(err).Msg("error when trying to close rows")
		}
	}()

	if !row.Next() {
		return nil, fmt.Errorf("there's no variable with the name '%s'", name)
	}

	var v uservariables.GlobalVariable

	err = row.Scan(
		&v.ID,
		&v.Name,
		&v.Content,
		&v.Type,
	)
	if err != nil {
		return nil, err
	}

	return &v, nil
}

func ulvExists(db *DatabaseInstance, name string) (bool, error) {
	i := db.GetSQLInstance()

	q := "SELECT name FROM variables_local WHERE name = ?;"

	row, err := i.Query(q, name)
	if err != nil {
		return false, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Err(err).Msg("Error when trying to close rows")
		}
	}()

	return row.Next(), nil
}

func ugvExists(db *DatabaseInstance, name string) (bool, error) {
	i := db.GetSQLInstance()

	q := "SELECT name FROM variables_global WHERE name = ?;"

	row, err := i.Query(q, name)
	if err != nil {
		return false, err
	}

	defer func() {
		err := row.Close()
		if err != nil {
			log.Err(err).Msg("Error when trying to close rows")
		}
	}()

	return row.Next(), nil
}
