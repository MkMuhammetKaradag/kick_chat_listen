package bootstrap

import (
	authHandlers "kick-chat/internal/handlers/auth"
	chatHandlers "kick-chat/internal/handlers/chat"
	authUsecase "kick-chat/internal/usecases/auth"
	chatUsecase "kick-chat/internal/usecases/chat"
)

// func SetupHTTPHandlers() map[string]interface{} {
// 	helloUseCase := usecase.NewhelloUseCase("naber")
// 	hellohandler := handlers.NewHelloHandler(helloUseCase)

//		return map[string]interface{}{
//			"hello": hellohandler,
//		}
//	}
type Handlers struct {
	Hello  *chatHandlers.HelloHandler
	Listen *chatHandlers.ListenHandler
	Signup *authHandlers.SignUpHandler
	Signin *authHandlers.SignInHandler
	// Diğer handler'lar
}

func SetupHTTPHandlers(postgresRepo PostgresRepository, sessionManager SessionManager) *Handlers {
	return &Handlers{
		Hello:  chatHandlers.NewHelloHandler(chatUsecase.NewhelloUseCase(postgresRepo, "naber")),
		Listen: chatHandlers.NewListenHandler(chatUsecase.NewListenUseCase(postgresRepo)),
		Signup: authHandlers.NewSignUpHandler(authUsecase.NewSignUpUseCase(postgresRepo)),
		Signin: authHandlers.NewSignInHandler(authUsecase.NewSignInUseCase(postgresRepo, sessionManager)),
	}
}
