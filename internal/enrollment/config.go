package enrollment

type Config struct {
	Port string `env:"PORT" envDefault:":50051"`
	IsProduction bool `env:"IS_PRODUCTION" envDefault:"false"`
	PublicKey string `env:"PUBLIC_KEY,required"`
	PrivateKey string `env:"PRIVATE_KEY,required"`
	EnrollmentDurationDays int `env:"ENROLLMENT_DURATION_DAYS" envDefault:"30"`
	PrivateKeyBytes []byte
}