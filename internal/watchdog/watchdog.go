package watchdog

/*
This package layers sits between user request and provider
We are going to take the following request :
{
	Token string
	ProviderName string
	DownstreamPath string
	Method string
	Headers map[string][]string
	Body []byte
}

route this to the provider
*/

type Watchdog struct {
	Token          string
	ProviderName   string
	DownstreamPath string
	Method         string
	Headers        map[string][]string
	Body           []byte
}

func (w *Watchdog) NewWatchDog() *Watchdog {
	return &Watchdog{
		Token:          w.Token,
		ProviderName:   w.ProviderName,
		DownstreamPath: w.DownstreamPath,
		Method:         w.Method,
		Headers:        w.Headers,
		Body:           w.Body,
	}
}

func (w *Watchdog) GetToken() string {
	if w.Token == "" {
		return "no token"
	}

	return w.Token
}
