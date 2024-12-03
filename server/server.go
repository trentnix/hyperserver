package server

type (
	ApplicationServer struct{}
)

func NewApplicationServer() *ApplicationServer {
	return &ApplicationServer{}
}

func (s *ApplicationServer) Shutdown() error {
	return nil
}
