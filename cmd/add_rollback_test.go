package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aolmosj/azsel/internal/config"
)

const testTenantID = "11111111-1111-1111-1111-111111111111"

// sandbox aísla la configuración de azsel para un test.
func addSandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv(config.EnvHome, home)
	return home
}

// El caso feliz, de punta a punta con un az de mentira que sale con 0.
func TestAddSucceeds(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err != nil {
		t.Fatalf("add: %v", err)
	}

	if fi, err := os.Stat(filepath.Join(home, "tenants", "acme")); err != nil || !fi.IsDir() {
		t.Fatalf("no se creó el directorio del tenant: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.FindTenant("acme")
	if got == nil {
		t.Fatal("el tenant no se guardó en config.json")
	}
	if got.TenantID != testTenantID {
		t.Errorf("TenantID = %q, quería %q", got.TenantID, testTenantID)
	}
}

// El fallo que motiva esta issue: az login falla y el directorio se queda.
func TestAddRemovesDirectoryItCreatedWhenLoginFails(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 1")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add devolvió nil con az login fallando")
	}

	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Errorf("quedó el directorio huérfano (err=%v)", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.FindTenant("acme") != nil {
		t.Error("config.json quedó con un tenant cuyo login no completó")
	}
}

// El contrapunto, y lo que hace peligroso este arreglo si se hace mal: un
// directorio preexistente puede contener credenciales válidas de un intento
// anterior. Un login fallido no debe destruirlas.
func TestAddKeepsPreexistingDirectoryWhenLoginFails(t *testing.T) {
	home := addSandbox(t)

	tenantDir := filepath.Join(home, "tenants", "acme")
	if err := os.MkdirAll(tenantDir, 0755); err != nil {
		t.Fatalf("preparando: %v", err)
	}
	token := filepath.Join(tenantDir, "msal_token_cache.json")
	if err := os.WriteFile(token, []byte(`{"credenciales":"valiosas"}`), 0600); err != nil {
		t.Fatalf("preparando: %v", err)
	}

	fakeAzureCLI(t, "exit 1")
	feedStdin(t, "acme\n"+testTenantID+"\n")
	quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add devolvió nil con az login fallando")
	}

	if _, err := os.Stat(tenantDir); err != nil {
		t.Fatalf("se borró un directorio preexistente: %v", err)
	}
	data, err := os.ReadFile(token)
	if err != nil {
		t.Fatalf("se borraron las credenciales preexistentes: %v", err)
	}
	if string(data) != `{"credenciales":"valiosas"}` {
		t.Errorf("las credenciales cambiaron: %s", data)
	}
}

// Un nombre inválido debe rechazarse sin crear nada y sin llegar a pedir el
// tenant ID.
func TestAddRejectsInvalidNameWithoutTouchingDisk(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "Acme Corp\n"+testTenantID+"\n")
	output := quiet(t)

	if err := run(t, newAddCmd()); err == nil {
		t.Fatal("add aceptó un nombre inválido")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants")); !os.IsNotExist(err) {
		t.Error("se creó el directorio de tenants pese al nombre inválido")
	}
	// El offset de stdin no sirve para comprobar esto: bufio.Reader lee por
	// bloques, no por líneas. Lo que sí prueba que no se siguió adelante es
	// que el segundo prompt nunca se imprimió.
	if got := output(); strings.Contains(got, "Azure Tenant ID") {
		t.Errorf("se pidió el tenant ID pese al nombre inválido:\n%s", got)
	}
}

// Un tenant ID mal pegado debe morir aquí, no en Azure: el error de azsel es
// más claro y no cuesta un viaje al navegador.
func TestAddRejectsInvalidTenantIDBeforeCallingAz(t *testing.T) {
	home := addSandbox(t)
	fakeAzureCLI(t, `touch "$AZSEL_HOME/az-called"; exit 0`)
	feedStdin(t, "acme\nno-soy-un-tenant\n")
	quiet(t)

	err := run(t, newAddCmd())
	if err == nil {
		t.Fatal("add aceptó un tenant ID inválido")
	}
	if !strings.Contains(err.Error(), "invalid tenant ID") {
		t.Errorf("error = %q, quería que explicara el formato", err)
	}
	if _, err := os.Stat(filepath.Join(home, "az-called")); !os.IsNotExist(err) {
		t.Error("se invocó az pese al tenant ID inválido")
	}
	if _, err := os.Stat(filepath.Join(home, "tenants", "acme")); !os.IsNotExist(err) {
		t.Error("se creó el directorio del tenant pese al tenant ID inválido")
	}
}

// La normalización tiene que llegar hasta config.json: si no, el mismo tenant
// escrito con distinta caja aparecería como dos entradas distintas.
func TestAddStoresTenantIDNormalized(t *testing.T) {
	addSandbox(t)
	fakeAzureCLI(t, "exit 0")
	feedStdin(t, "acme\nAABBCCDD-1122-3344-5566-778899AABBCC\n")
	quiet(t)

	if err := run(t, newAddCmd()); err != nil {
		t.Fatalf("add: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tenant := cfg.FindTenant("acme")
	if tenant == nil {
		t.Fatal("no se guardó el tenant")
	}
	if want := "aabbccdd-1122-3344-5566-778899aabbcc"; tenant.TenantID != want {
		t.Errorf("TenantID = %q, quería %q", tenant.TenantID, want)
	}
}
