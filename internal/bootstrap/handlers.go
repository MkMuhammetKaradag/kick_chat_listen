package bootstrap

import (
	authHandlers "kick-chat/internal/handlers/auth"
	chatHandlers "kick-chat/internal/handlers/chat"
	authUsecase "kick-chat/internal/usecases/auth"
	chatUsecase "kick-chat/internal/usecases/chat"
	"log"
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
	listenUseCase := chatUsecase.NewListenUseCase(postgresRepo)
	if err := listenUseCase.StartActiveListenersOnStartup(); err != nil {
		log.Printf("Aktif dinleyicileri başlatırken hata: %v", err)
		// Hata kritik değilse fatal olmayabilir, loglayıp devam edebiliriz.
	}
	return &Handlers{
		Hello:  chatHandlers.NewHelloHandler(chatUsecase.NewhelloUseCase(postgresRepo, "naber")),
		Listen: chatHandlers.NewListenHandler(listenUseCase),
		Signup: authHandlers.NewSignUpHandler(authUsecase.NewSignUpUseCase(postgresRepo)),
		Signin: authHandlers.NewSignInHandler(authUsecase.NewSignInUseCase(postgresRepo, sessionManager)),
	}
}
