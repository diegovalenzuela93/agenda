package main

import (
	"database/sql"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

//go:embed templates static
var archivos embed.FS

type Contacto struct {
	ID       int
	Nombre   string
	Apellido string
	Telefono string
	Email    string
	CreadoEn time.Time
}

var db *sql.DB
var tmpl *template.Template

func conectarDB() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=agenda password=agenda dbname=agenda sslmode=disable"
	}

	var err error
	for i := 0; i < 10; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			if err = db.Ping(); err == nil {
				log.Println("Conectado a PostgreSQL")
				return
			}
		}
		log.Printf("Esperando base de datos... intento %d/10", i+1)
		time.Sleep(2 * time.Second)
	}
	log.Fatal("No se pudo conectar a la base de datos:", err)
}

func crearTabla() {
	query := `
	CREATE TABLE IF NOT EXISTS contactos (
		id        SERIAL PRIMARY KEY,
		nombre    VARCHAR(100) NOT NULL,
		apellido  VARCHAR(100) NOT NULL,
		telefono  VARCHAR(30),
		email     VARCHAR(150),
		creado_en TIMESTAMP DEFAULT NOW()
	)`
	if _, err := db.Exec(query); err != nil {
		log.Fatal("Error creando tabla:", err)
	}
}

func obtenerTodos() ([]Contacto, error) {
	rows, err := db.Query(`SELECT id, nombre, apellido, COALESCE(telefono,''), COALESCE(email,''), creado_en FROM contactos ORDER BY apellido, nombre`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []Contacto
	for rows.Next() {
		var ct Contacto
		if err := rows.Scan(&ct.ID, &ct.Nombre, &ct.Apellido, &ct.Telefono, &ct.Email, &ct.CreadoEn); err != nil {
			return nil, err
		}
		lista = append(lista, ct)
	}
	return lista, nil
}

// GET /
func vistaInicio(c *gin.Context) {
	contactos, err := obtenerTodos()
	data := gin.H{"Contactos": contactos, "Error": ""}
	if err != nil {
		data["Error"] = err.Error()
	}
	c.Status(http.StatusOK)
	tmpl.ExecuteTemplate(c.Writer, "index.html", data)
}

// POST /contactos
func vistaCrear(c *gin.Context) {
	nombre := c.PostForm("nombre")
	apellido := c.PostForm("apellido")
	telefono := c.PostForm("telefono")
	email := c.PostForm("email")

	if nombre == "" || apellido == "" {
		contactos, _ := obtenerTodos()
		tmpl.ExecuteTemplate(c.Writer, "index.html", gin.H{
			"Contactos": contactos,
			"Error":     "Nombre y apellido son obligatorios",
		})
		return
	}

	_, err := db.Exec(
		`INSERT INTO contactos (nombre, apellido, telefono, email) VALUES ($1,$2,$3,$4)`,
		nombre, apellido, telefono, email,
	)
	if err != nil {
		contactos, _ := obtenerTodos()
		tmpl.ExecuteTemplate(c.Writer, "index.html", gin.H{
			"Contactos": contactos,
			"Error":     err.Error(),
		})
		return
	}
	c.Redirect(http.StatusSeeOther, "/")
}

// GET /contactos/:id/editar
func vistaEditar(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var ct Contacto
	err := db.QueryRow(
		`SELECT id, nombre, apellido, COALESCE(telefono,''), COALESCE(email,'') FROM contactos WHERE id=$1`, id,
	).Scan(&ct.ID, &ct.Nombre, &ct.Apellido, &ct.Telefono, &ct.Email)
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/")
		return
	}
	tmpl.ExecuteTemplate(c.Writer, "editar.html", ct)
}

// POST /contactos/:id/editar
func vistaActualizar(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	nombre := c.PostForm("nombre")
	apellido := c.PostForm("apellido")
	telefono := c.PostForm("telefono")
	email := c.PostForm("email")

	db.Exec(
		`UPDATE contactos SET nombre=$1, apellido=$2, telefono=$3, email=$4 WHERE id=$5`,
		nombre, apellido, telefono, email, id,
	)
	c.Redirect(http.StatusSeeOther, "/")
}

// POST /contactos/:id/eliminar
func vistaEliminar(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	db.Exec(`DELETE FROM contactos WHERE id=$1`, id)
	c.Redirect(http.StatusSeeOther, "/")
}

func main() {
	conectarDB()
	crearTabla()

	tmpl = template.Must(template.ParseFS(archivos, "templates/*.html"))

	r := gin.Default()

	r.StaticFS("/static", http.FS(archivos))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"estado": "ok"})
	})

	r.GET("/", vistaInicio)
	r.POST("/contactos", vistaCrear)
	r.GET("/contactos/:id/editar", vistaEditar)
	r.POST("/contactos/:id/editar", vistaActualizar)
	r.POST("/contactos/:id/eliminar", vistaEliminar)

	// API JSON (mantiene compatibilidad)
	api := r.Group("/api/contactos")
	{
		api.GET("", apiListar)
		api.POST("", apiCrear)
		api.PUT("/:id", apiActualizar)
		api.DELETE("/:id", apiEliminar)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Servidor en http://localhost:%s", port)
	r.Run(":" + port)
}

// --- API JSON ---

func apiListar(c *gin.Context) {
	contactos, err := obtenerTodos()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, contactos)
}

func apiCrear(c *gin.Context) {
	var ct Contacto
	if err := c.ShouldBindJSON(&ct); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := db.QueryRow(
		`INSERT INTO contactos (nombre, apellido, telefono, email) VALUES ($1,$2,$3,$4) RETURNING id, creado_en`,
		ct.Nombre, ct.Apellido, ct.Telefono, ct.Email,
	).Scan(&ct.ID, &ct.CreadoEn)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, ct)
}

func apiActualizar(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var ct Contacto
	if err := c.ShouldBindJSON(&ct); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := db.Exec(
		`UPDATE contactos SET nombre=$1, apellido=$2, telefono=$3, email=$4 WHERE id=$5`,
		ct.Nombre, ct.Apellido, ct.Telefono, ct.Email, id,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No encontrado"})
		return
	}
	ct.ID = id
	c.JSON(http.StatusOK, ct)
}

func apiEliminar(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	res, err := db.Exec(`DELETE FROM contactos WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "No encontrado"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mensaje": "Eliminado"})
}
