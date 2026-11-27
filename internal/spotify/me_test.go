// Testes do UserID (me.go).
//
// FERRAMENTAS que você já tem (estão em client_test.go, mesmo pacote):
//
//	newTestClient(t, store, handler) -> (*Client, *atomic.Int32)
//	    monta um Client falando com um "Spotify de mentira". O handler é o que
//	    responde a GET /me; ele recebe o r *http.Request para você conferir
//	    caminho e headers, e o w para escrever a resposta.
//	&fakeTokenStore{}                -> banco de mentira VAZIO (Load dá ErrNoToken)
//	&fakeTokenStore{tok: validToken(), has: true} -> com um token "valido"
//
// PARA RESPONDER no handler:
//
//	w.Write([]byte(`{"id":"user123"}`))                    // 200 com JSON
//	w.WriteHeader(http.StatusForbidden); w.Write(...)       // outro status: WriteHeader ANTES do Write
//
// PARA CONFERIR no handler (use t.Fatalf, e não t.Fatalf: o handler roda em
// outra goroutine, e Fatalf só é seguro na goroutine do teste):
//
//	r.URL.Path                       -> tem que ser "/me"
//	r.Header.Get("Authorization")    -> tem que ser "Bearer abc"
//
// Para testar erro de tipo específico (*APIError):
//
//	var apiErr *APIError
//	if !errors.As(err, &apiErr) { ... }   // e depois confira apiErr.Status
package spotify

import (
	"errors"
	"net/http"
	"testing"
)

// TESTE a + b (juntos, e é de propósito) — sucesso, com o token do PARÂMETRO.
//
//  1. Crie um fakeTokenStore VAZIO: &fakeTokenStore{}.
//  2. O handler confere o caminho ("/me") e o header ("Bearer abc") e responde
//     {"id":"user123"}.
//  3. Chame client.UserID(ctx, "abc") e confira que devolveu "user123", sem erro.
//  4. Confira também que o banco NÃO foi tocado: store.saves == 0.
//
// Se passar com o store vazio, provado: a função usou o token do parâmetro e
// não leu o banco. (Antes de apagar as 4 linhas do getWithToken que ainda
// buscam o token no banco, este teste deve FALHAR com ErrNoToken.)
func TestUserID(t *testing.T) {
	store := &fakeTokenStore{}
	client, _ := newTestClient(t, store, func(w http.ResponseWriter, r *http.Request) {

		if r.URL.Path != "/me" {
			// Errorf (e não Fatalf): o handler roda em OUTRA goroutine, e
			// Fatalf só é seguro na goroutine do teste.
			t.Errorf("path = %v, want /me", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer abc" {
			t.Errorf("header = %v, want Bearer abc", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"id":"user123"}`))
	})

	user, err := client.UserID(t.Context(), "abc")

	if err != nil {
		t.Fatalf("UserID() error = %v, wantErr nil", err)
	}
	if user != "user123" {
		t.Fatalf("UserID() = %v, want %v", user, "user123")
	}
	if store.saves != 0 {
		t.Fatalf("UserID() saves = %v, want %v", store.saves, 0)
	}

}

// TESTES c, d, e — o Spotify responde algo que NÃO é um perfil válido.
// Todos devolvem erro e o id vazio. Use UMA tabela (table-driven), como em
// TestRefresh do auth_test.go. Campos sugeridos:
//
//	name      string
//	status    int      // 0 = não chama WriteHeader (fica 200)
//	body      string
//	wantAPIErr bool    // true: o erro tem que ser *APIError (errors.As)
//
// Casos:
//
//	"forbidden"   status 403, body `{"error":"forbidden"}`  -> *APIError, Status 403
//	"empty id"    200,        body `{}`                     -> erro (id vazio)
//	"invalid json" 200,       body `not json`               -> erro
//
// Em todos: err != nil e o id devolvido == "".
//
// EXTRA (opcional, vale a pena): o caso 204 (WriteHeader(http.StatusNoContent),
// sem corpo) -> o UserID tem que devolver erro, e não "" sem erro.
func TestUserIDErrors(t *testing.T) {

	type testCase struct {
		name       string
		status     int // 0 = não chama WriteHeader (fica 200)
		body       string
		wantAPIErr bool // true: o erro tem que ser *APIError (errors.As)
	}

	tests := []testCase{
		{
			name:       "forbidden",
			status:     403,
			body:       `{"error":"forbidden"}`,
			wantAPIErr: true,
		},
		{
			name:       "empty id",
			status:     200,
			body:       `{}`,
			wantAPIErr: false,
		},
		{
			name:       "invalid json",
			status:     200,
			body:       `not json`,
			wantAPIErr: false,
		},
		{
			name:       "no content",
			status:     204,
			body:       "",
			wantAPIErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, &fakeTokenStore{}, func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				w.Write([]byte(tc.body))
			})
			user, err := client.UserID(t.Context(), "abc")

			// Todos os casos DEVEM dar erro. Sem esta checagem, um UserID que
			// devolvesse ("", nil) passaria despercebido (era o buraco que a
			// mutação "remover a checagem de id vazio" revelou).
			if err == nil {
				t.Fatalf("UserID() error = nil, want an error")
			}
			// E nunca devolve um id junto com o erro.
			if user != "" {
				t.Errorf("UserID() = %q, want empty", user)
			}
			// Só o 403 vem do Spotify: esse tem que ser *APIError, com o status.
			if tc.wantAPIErr {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("error = %v, want *APIError", err)
				}
				if apiErr.Status != tc.status {
					t.Errorf("status = %d, want %d", apiErr.Status, tc.status)
				}
			}
		})
	}
}
