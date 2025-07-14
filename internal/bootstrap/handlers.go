package bootstrap

import (
	handlers "kick-chat/internal/handlers/chat"
	usecase "kick-chat/internal/usecases/chat"
)

// func SetupHTTPHandlers() map[string]interface{} {
// 	helloUseCase := usecase.NewhelloUseCase("naber")
// 	hellohandler := handlers.NewHelloHandler(helloUseCase)

//		return map[string]interface{}{
//			"hello": hellohandler,
//		}
//	}
type Handlers struct {
	Hello *handlers.HelloHandler
	// Diğer handler'lar
}

func SetupHTTPHandlers() *Handlers {
	return &Handlers{
		Hello: handlers.NewHelloHandler(usecase.NewhelloUseCase("naber")),
	}
}
