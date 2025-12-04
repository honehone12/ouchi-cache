package cache

import "hash/fnv"

func HashKey(key string) (string, error) {
	hasher := fnv.New128a()
	if _, err := hasher.Write([]byte(key)); err != nil {
		return "", err
	}
	hash := string(hasher.Sum(nil))
	return hash, nil
}
