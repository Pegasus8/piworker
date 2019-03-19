package generic

// Element is the generic struct used by structs Trigger and Action
type Element struct {
	ID string
	Name string
	Description string
	RunFunc func([]Arg) (bool, error)
	Args []Arg
}

// Arg is the struct that defines every argument received by any Element type.
type Arg struct {
	ID string
	Name string
	Description string
	Content interface{}
	ContentType string
}

// Run is a method of the Element struct that executes the main function of the element in question
func (e *Element) Run(args []Arg) (bool, error) {
	result, err := e.RunFunc(args)
	if err != nil {
		return false, err
	}
	return result, nil
}