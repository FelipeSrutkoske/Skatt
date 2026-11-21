import { useEffect, useRef } from 'react'

const VERTEX_SHADER = `
attribute vec2 a_position;
varying vec2 v_uv;
void main() {
  v_uv = a_position * 0.5 + 0.5;
  gl_Position = vec4(a_position, 0.0, 1.0);
}
`

const FRAGMENT_SHADER = `
precision mediump float;

varying vec2 v_uv;
uniform float u_time;
uniform vec2 u_resolution;

float hash(vec2 p) {
  return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453123);
}

float noise(vec2 p) {
  vec2 i = floor(p);
  vec2 f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  float a = hash(i);
  float b = hash(i + vec2(1.0, 0.0));
  float c = hash(i + vec2(0.0, 1.0));
  float d = hash(i + vec2(1.0, 1.0));
  return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

float fbm(vec2 p) {
  float value = 0.0;
  float amplitude = 0.52;
  mat2 rot = mat2(0.80, 0.60, -0.60, 0.80);
  for (int i = 0; i < 5; i++) {
    value += amplitude * noise(p);
    p = rot * p * 2.03 + vec2(1.7, 9.2);
    amplitude *= 0.5;
  }
  return value;
}

void main() {
  vec2 st = v_uv;
  float aspect = u_resolution.x / max(u_resolution.y, 1.0);
  vec2 p = vec2((st.x - 0.5) * aspect, st.y - 0.5) * 1.45;

  float t = u_time * 0.075;

  vec2 q = vec2(
    fbm(p + vec2(0.0, 0.0) + vec2(t * 0.65, -t * 0.45)),
    fbm(p + vec2(5.2, 1.3) + vec2(-t * 0.5, t * 0.35))
  );

  vec2 r = vec2(
    fbm(p + 2.8 * q + vec2(1.7, 9.2) + vec2(t * 0.35, t * 0.25)),
    fbm(p + 2.8 * q + vec2(8.3, 2.8) + vec2(-t * 0.3, -t * 0.2))
  );

  float f = fbm(p + 2.35 * r);

  vec3 cPaper = vec3(0.914, 0.925, 0.937);
  vec3 cSoft = vec3(0.871, 0.886, 0.902);
  vec3 cMist = vec3(0.808, 0.831, 0.855);
  vec3 cWash = vec3(0.678, 0.710, 0.741);
  vec3 cSlate = vec3(0.424, 0.459, 0.490);

  vec3 color = mix(cPaper, cSoft, smoothstep(0.15, 0.55, f));
  color = mix(color, cMist, smoothstep(0.30, 0.75, length(q)));
  color = mix(color, cWash, smoothstep(0.32, 0.80, r.x) * 0.78);
  color = mix(color, cSlate, smoothstep(0.48, 0.88, f * r.y) * 0.48);

  float rim = smoothstep(0.18, 0.0, abs(f - 0.54));
  color = mix(color, cWash, rim * 0.22);

  float grain = (hash(gl_FragCoord.xy + floor(u_time * 4.0) * 0.13) - 0.5) * 0.015;
  color = clamp(color * 0.955 + grain, 0.0, 1.0);

  gl_FragColor = vec4(color, 1.0);
}
`

function compileShader(gl: WebGLRenderingContext, type: number, source: string): WebGLShader | null {
  const shader = gl.createShader(type)
  if (!shader) {
    return null
  }
  gl.shaderSource(shader, source)
  gl.compileShader(shader)
  if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
    gl.deleteShader(shader)
    return null
  }
  return shader
}

export function WatercolorBackground() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) {
      return
    }

    const gl = canvas.getContext('webgl', {
      alpha: false,
      antialias: false,
      depth: false,
      stencil: false,
      preserveDrawingBuffer: false,
      powerPreference: 'low-power',
    })
    if (!gl) {
      return
    }

    const vertexShader = compileShader(gl, gl.VERTEX_SHADER, VERTEX_SHADER)
    const fragmentShader = compileShader(gl, gl.FRAGMENT_SHADER, FRAGMENT_SHADER)
    if (!vertexShader || !fragmentShader) {
      return
    }

    const program = gl.createProgram()
    if (!program) {
      return
    }
    gl.attachShader(program, vertexShader)
    gl.attachShader(program, fragmentShader)
    gl.linkProgram(program)
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      gl.deleteProgram(program)
      return
    }

    const buffer = gl.createBuffer()
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer)
    gl.bufferData(
      gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 1, -1, -1, 1, -1, 1, 1, -1, 1, 1]),
      gl.STATIC_DRAW,
    )

    const positionLocation = gl.getAttribLocation(program, 'a_position')
    const timeLocation = gl.getUniformLocation(program, 'u_time')
    const resolutionLocation = gl.getUniformLocation(program, 'u_resolution')

    gl.useProgram(program)
    gl.enableVertexAttribArray(positionLocation)
    gl.vertexAttribPointer(positionLocation, 2, gl.FLOAT, false, 0, 0)

    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 1.25)
      const width = Math.max(1, Math.floor(window.innerWidth * dpr * 0.75))
      const height = Math.max(1, Math.floor(window.innerHeight * dpr * 0.75))
      if (canvas.width !== width || canvas.height !== height) {
        canvas.width = width
        canvas.height = height
        gl.viewport(0, 0, width, height)
      }
    }

    resize()
    window.addEventListener('resize', resize)

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    let rafId = 0
    const startTime = performance.now()
    let lastDraw = 0

    const renderFrame = (now: number) => {
      if (!reducedMotion && now - lastDraw < 33) {
        rafId = window.requestAnimationFrame(renderFrame)
        return
      }
      lastDraw = now
      const elapsed = reducedMotion ? 14.0 : (now - startTime) * 0.001 + 6.0
      gl.uniform1f(timeLocation, elapsed)
      gl.uniform2f(resolutionLocation, canvas.width, canvas.height)
      gl.drawArrays(gl.TRIANGLES, 0, 6)

      if (!reducedMotion) {
        rafId = window.requestAnimationFrame(renderFrame)
      }
    }

    rafId = window.requestAnimationFrame(renderFrame)

    return () => {
      window.cancelAnimationFrame(rafId)
      window.removeEventListener('resize', resize)
      gl.deleteBuffer(buffer)
      gl.deleteProgram(program)
      gl.deleteShader(vertexShader)
      gl.deleteShader(fragmentShader)
    }
  }, [])

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className="no-print fixed inset-0 w-full h-full z-0 pointer-events-none"
    />
  )
}
