package accesspwd

import "golang.org/x/crypto/bcrypt"

// Cost is the bcrypt work factor. Tests may lower it.
var Cost = bcrypt.DefaultCost

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), Cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func Verify(hash, password string) bool {
	if hash == "" {
		return password == ""
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
