package crypto

import (
	"context"
	"fmt"
	"reflect"

	"gorm.io/gorm/schema"
)

// EncryptedSerializer is a GORM serializer that transparently encrypts string
// fields tagged with `gorm:"serializer:enc"` when writing and decrypts on read.
// Register once at startup: schema.RegisterSerializer("enc", EncryptedSerializer{Cipher: c})
type EncryptedSerializer struct {
	Cipher *Cipher
}

// Value encrypts the field value before it is written to the database.
// Returns []byte (bytea column) or nil for empty strings.
func (s EncryptedSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue interface{}) (interface{}, error) {
	str, ok := fieldValue.(string)
	if !ok {
		return nil, fmt.Errorf("enc serializer: expected string, got %T", fieldValue)
	}
	if str == "" {
		return nil, nil
	}
	blob, err := s.Cipher.Encrypt([]byte(str))
	if err != nil {
		return nil, fmt.Errorf("enc serializer: encrypt: %w", err)
	}
	return blob, nil
}

// Scan decrypts the raw database value and sets it on the destination field.
func (s EncryptedSerializer) Scan(_ context.Context, field *schema.Field, dst reflect.Value, dbValue interface{}) error {
	var decrypted string

	if dbValue != nil {
		var raw []byte
		switch v := dbValue.(type) {
		case []byte:
			raw = v
		case string:
			raw = []byte(v)
		default:
			return fmt.Errorf("enc serializer: unexpected db type %T", dbValue)
		}
		if len(raw) > 0 {
			plaintext, err := s.Cipher.Decrypt(raw)
			if err != nil {
				return fmt.Errorf("enc serializer: decrypt: %w", err)
			}
			decrypted = string(plaintext)
		}
	}

	field.ReflectValueOf(context.Background(), dst).SetString(decrypted)
	return nil
}
